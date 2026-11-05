package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TeamRepository interface {
	Save(ctx context.Context, team *domain.Team) error
	List(ctx context.Context, filter domain.TeamFilter) ([]domain.Team, int, error)
}

type TeamMemberRepository interface {
	Save(ctx context.Context, team *domain.TeamMember) error
	GetByTeamAndUserID(ctx context.Context, teamID uuid.UUID, userID uuid.UUID) (*domain.TeamMember, error)
	GetByTeamAndUserIDForUpdate(ctx context.Context, teamID uuid.UUID, userID uuid.UUID) (*domain.TeamMember, error)
	UpdateRole(ctx context.Context, teamID uuid.UUID, userID uuid.UUID, role domain.TeamRole) error
}

type UsrRepository interface {
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type TeamService struct {
	tx  TransactionManager
	tr  TeamRepository
	tmr TeamMemberRepository
	usr UsrRepository
	log *slog.Logger
}

func NewTeamService(tx TransactionManager, tr TeamRepository, tmr TeamMemberRepository, usr UsrRepository, log *slog.Logger) *TeamService {
	return &TeamService{
		tx:  tx,
		tr:  tr,
		tmr: tmr,
		usr: usr,
		log: log,
	}
}

type CreateTeamInput struct {
	Name string
}

func (t *TeamService) Create(
	ctx context.Context,
	creatorID uuid.UUID,
	input CreateTeamInput,
) (*domain.Team, error) {
	name, err := nameNormalize(input.Name)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	if creatorID == uuid.Nil {
		return nil, fmt.Errorf("create team: %w: creator ID must not be nil", domain.ErrInvalidInput)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	newTeam := &domain.Team{
		ID:        uuid.New(),
		Name:      name,
		CreatedAt: now,
		CreatedBy: creatorID,
	}
	newTeamMember := &domain.TeamMember{
		TeamID:   newTeam.ID,
		UserID:   creatorID,
		Role:     domain.OwnerRole,
		JoinedAt: now,
	}
	t.log.Debug("Team: create a new team", "team", name)
	err = t.tx.Do(ctx, func(txCtx context.Context) error {
		if err := t.tr.Save(txCtx, newTeam); err != nil {
			return err
		}
		if err := t.tmr.Save(txCtx, newTeamMember); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	t.log.Debug("Team: create a new team sucessfully", "team", name)
	return newTeam, nil
}

func (t *TeamService) List(ctx context.Context, actorID uuid.UUID, filter domain.TeamFilter) ([]domain.Team, int, error) {
	if actorID == uuid.Nil {
		return nil, 0, fmt.Errorf("get team list: %w: user_id must not be nil", domain.ErrInvalidInput)
	}
	if err := validateLimit(filter.Limit); err != nil {
		return nil, 0, fmt.Errorf("get team list: %w", err)
	}
	if err := validateOffset(filter.Offset); err != nil {
		return nil, 0, fmt.Errorf("get team list: %w", err)
	}
	filter.UserID = &actorID

	t.log.Debug("Team: get list of team", "filter", filter)
	teams, total, err := t.tr.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("get team list: %w: request error", err)
	}
	t.log.Debug("Team: get list of team sucessfully", "filter", filter, "total", total)
	return teams, total, nil
}

type AddTeamMemberInput struct {
	Email string
	Role  domain.TeamRole
}

func (t *TeamService) AddMember(
	ctx context.Context,
	actorID uuid.UUID,
	teamID uuid.UUID,
	input AddTeamMemberInput,
) (*domain.TeamMember, error) {
	if actorID == uuid.Nil {
		return nil, fmt.Errorf("invite team: %w: actor ID must not be nil", domain.ErrInvalidInput)
	}
	if teamID == uuid.Nil {
		return nil, fmt.Errorf("invite team: %w: team ID must not be nil", domain.ErrInvalidInput)
	}
	email, err := emailNormalize(input.Email)
	if err != nil {
		return nil, fmt.Errorf("invite team: validate email: %w", err)
	}
	if input.Role != domain.AdminRole && input.Role != domain.MemberRole {
		return nil, fmt.Errorf("invite team: %w: role must not be nil and not equal owner", domain.ErrInvalidInput)
	}
	t.log.Debug("Team: invite customer to team", "email", input.Email, "role", input.Role)

	var candidateMember *domain.TeamMember
	err = t.tx.Do(ctx, func(txCtx context.Context) error {
		actor, err := t.tmr.GetByTeamAndUserIDForUpdate(txCtx, teamID, actorID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrForbidden
			}
			return fmt.Errorf("get actor membership: %w", err)
		}
		if actor.Role != domain.OwnerRole && actor.Role != domain.AdminRole {
			return domain.ErrForbidden
		}

		candidate, err := t.usr.GetByEmail(txCtx, email)
		if err != nil {
			return err
		}

		_, err = t.tmr.GetByTeamAndUserID(txCtx, teamID, candidate.ID)
		switch {
		case err == nil:
			return domain.ErrTeamMemberAlreadyExists
		case errors.Is(err, domain.ErrTeamMemberNotFound):
		default:
			return fmt.Errorf("check candidate membership: %w", err)
		}

		candidateMember = &domain.TeamMember{
			TeamID:   teamID,
			UserID:   candidate.ID,
			Role:     input.Role,
			JoinedAt: time.Now().UTC().Truncate(time.Millisecond),
		}
		err = t.tmr.Save(txCtx, candidateMember)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("invite team: %w", err)
	}
	t.log.Debug("Team: invite customer to team sucessfully", "email", input.Email, "role", input.Role)
	return candidateMember, nil
}

func (t *TeamService) UpdateRole(
	ctx context.Context,
	actorID, teamID, userID uuid.UUID,
	role domain.TeamRole,
) error {
	if actorID == uuid.Nil {
		return fmt.Errorf("change role team: %w: actor ID must not be nil", domain.ErrInvalidInput)
	}
	if teamID == uuid.Nil {
		return fmt.Errorf("change role team: %w: team ID must not be nil", domain.ErrInvalidInput)
	}
	if userID == uuid.Nil {
		return fmt.Errorf("change role team: %w: user ID must not be nil", domain.ErrInvalidInput)
	}
	if role != domain.AdminRole && role != domain.MemberRole {
		return fmt.Errorf("change role team: %w: role must not be nil and not equal owner", domain.ErrInvalidInput)
	}
	t.log.Debug("Team: Change role team", "actor_id", actorID, "team_id", teamID, "user_id", userID, "role", role)
	err := t.tx.Do(ctx, func(txCtx context.Context) error {
		actor, err := t.tmr.GetByTeamAndUserIDForUpdate(txCtx, teamID, actorID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrForbidden
			}
			return fmt.Errorf("get actor membership: %w", err)
		}
		if actor.Role != domain.OwnerRole {
			return domain.ErrForbidden
		}

		candidate, err := t.tmr.GetByTeamAndUserIDForUpdate(txCtx, teamID, userID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrTeamMemberNotFound
			}
			return fmt.Errorf("get candidate membership: %w", err)
		}
		if candidate.Role == domain.OwnerRole {
			return domain.ErrForbidden
		}

		if err := t.tmr.UpdateRole(txCtx, teamID, userID, role); err != nil {
			return fmt.Errorf("change role team: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("change role team: %w", err)
	}
	t.log.Debug("Team: Change role team sucessfully", "actor_id", actorID, "team_id", teamID, "user_id", userID, "role", role)
	return nil
}
