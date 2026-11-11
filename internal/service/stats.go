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

const (
	statsPeriodDays = 30
	statsTopLimit   = 3
)

type StatsRepository interface {
	Get(
		ctx context.Context,
		teamID uuid.UUID,
		since time.Time,
		qtyToppers int,
	) (*domain.TeamStats, error)
}

type StatsMembershipRepository interface {
	GetByTeamAndUserID(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error)
}

type StatsService struct {
	sr  StatsRepository
	tmr StatsMembershipRepository
	log *slog.Logger
}

func NewStatsService(sr StatsRepository, tmr StatsMembershipRepository, log *slog.Logger) *StatsService {
	return &StatsService{
		sr:  sr,
		tmr: tmr,
		log: log,
	}
}

func (s *StatsService) Get(ctx context.Context, actorID, teamID uuid.UUID) (*domain.TeamStats, error) {
	if actorID == uuid.Nil {
		return nil, fmt.Errorf("get stats: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if teamID == uuid.Nil {
		return nil, fmt.Errorf("get stats: %w: team_id must not be nil", domain.ErrInvalidInput)
	}
	actor, err := s.tmr.GetByTeamAndUserID(ctx, teamID, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrTeamMemberNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, fmt.Errorf("get stats: %w", err)
	}
	if actor.Role != domain.OwnerRole && actor.Role != domain.AdminRole {
		return nil, domain.ErrForbidden
	}
	since := time.Now().UTC().AddDate(0, 0, -statsPeriodDays)
	s.log.Debug("Stats: get data", "teamId", teamID, "since", since)
	stats, err := s.sr.Get(ctx, teamID, since, statsTopLimit)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w: request error", err)
	}
	s.log.Debug("Stats: get data successfully", "teamId", teamID, "since", since, "stats", stats)
	return stats, nil
}
