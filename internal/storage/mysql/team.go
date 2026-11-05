package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TeamRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTeamRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TeamRepository {
	return &TeamRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (t *TeamRepository) Save(ctx context.Context, team *domain.Team) error {
	t.log.Debug("MySQL: Team: Save team data", "name", team.Name, "id", team.ID, "user_id", team.CreatedBy)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	_, err := executor.ExecContext(ctx,
		"INSERT INTO teams (id, name, created_at, created_by) VALUES (?, ?, ?, ?)",
		uuidToBytes(team.ID), team.Name, team.CreatedAt, uuidToBytes(team.CreatedBy),
	)
	if err != nil {
		return fmt.Errorf("MySQL: Team: Failed to save team data: %w", err)
	}
	t.log.Debug("MySQL: Team: Team data saved successfully")
	return nil
}

type rawTeam struct {
	ID        []byte    `db:"id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	CreatedBy []byte    `db:"created_by"`
}

func (t *TeamRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.Team, error) {
	t.log.Debug("MySQL: Team: Gets teams by user_id", "id", userID)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	var rawTeams []rawTeam
	err := executor.SelectContext(
		ctx,
		&rawTeams,
		`SELECT t.id, t.name, t.created_at, t.created_by
		FROM teams AS t
		JOIN team_members AS tm ON tm.team_id = t.id
		WHERE tm.user_id = ?
		ORDER BY t.created_at DESC, t.id DESC`,
		uuidToBytes(userID),
	)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team: Failed to get list of teams by user_id: %w", err)
	}
	teams := make([]domain.Team, 0, len(rawTeams))
	for _, r := range rawTeams {
		t, err := rawToTeam(&r)
		if err != nil {
			return nil, fmt.Errorf("MySQL: Team: failed to parse team row: %w", err)
		}
		teams = append(teams, *t)
	}
	t.log.Debug("MySQL: Team: Gets team data for user_id successfully", "user_id", userID, "team_count", len(teams))
	return teams, nil
}

func rawToTeam(r *rawTeam) (*domain.Team, error) {
	id, err := bytesToUUID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team: Wrong parse team id: %w", err)
	}
	createdBy, err := bytesToUUID(r.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team: Wrong parse created by: %w", err)
	}
	return &domain.Team{
		ID:        id,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
		CreatedBy: createdBy,
	}, nil
}
