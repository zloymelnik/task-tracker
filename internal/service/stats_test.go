package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type statsRepositoryFunc func(
	ctx context.Context,
	teamID uuid.UUID,
	since time.Time,
	limit int,
) (*domain.TeamStats, error)

func (f statsRepositoryFunc) Get(
	ctx context.Context,
	teamID uuid.UUID,
	since time.Time,
	limit int,
) (*domain.TeamStats, error) {
	return f(ctx, teamID, since, limit)
}

type statsMembershipRepositoryFunc func(
	ctx context.Context,
	teamID uuid.UUID,
	userID uuid.UUID,
) (*domain.TeamMember, error)

func (f statsMembershipRepositoryFunc) GetByTeamAndUserID(
	ctx context.Context,
	teamID uuid.UUID,
	userID uuid.UUID,
) (*domain.TeamMember, error) {
	return f(ctx, teamID, userID)
}

func TestStatsServiceGetAllowedRoles(t *testing.T) {
	for _, role := range []domain.TeamRole{
		domain.OwnerRole,
		domain.AdminRole,
	} {
		t.Run(string(role), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			actorID, teamID := uuid.New(), uuid.New()

			want := &domain.TeamStats{TeamID: teamID}

			membershipCalls := 0
			members := statsMembershipRepositoryFunc(func(
				gotCtx context.Context,
				gotTeamID, gotUserID uuid.UUID,
			) (*domain.TeamMember, error) {
				membershipCalls++

				if gotCtx != ctx || gotTeamID != teamID || gotUserID != actorID {
					t.Error("membership received unexpected context or IDs")
				}

				return &domain.TeamMember{Role: role}, nil
			})

			statsCalls := 0
			earliestSince := time.Now().UTC().AddDate(0, 0, -30)
			repo := statsRepositoryFunc(func(
				gotCtx context.Context,
				gotTeamID uuid.UUID,
				since time.Time,
				limit int,
			) (*domain.TeamStats, error) {
				statsCalls++

				if gotCtx != ctx || gotTeamID != teamID {
					t.Error("stats received unexpected context or team ID")
				}
				if limit != 3 {
					t.Errorf("limit = %d, want 3", limit)
				}
				latestSince := time.Now().UTC().AddDate(0, 0, -30)
				if since.Before(earliestSince) || since.After(latestSince) {
					t.Errorf("since = %s, want between %s and %s", since, earliestSince, latestSince)
				}

				return want, nil
			})

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := service.NewStatsService(repo, members, log)

			got, err := svc.Get(ctx, actorID, teamID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != want {
				t.Errorf("Get returned %v, want %v", got, want)
			}
			if membershipCalls != 1 || statsCalls != 1 {
				t.Errorf("calls: membership=%d stats=%d, want 1 each", membershipCalls, statsCalls)
			}
		})
	}
}

func TestStatsServiceGetMemberForbidden(t *testing.T) {
	ctx := context.Background()
	actorID, teamID := uuid.New(), uuid.New()

	members := statsMembershipRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*domain.TeamMember, error) {
		return &domain.TeamMember{Role: domain.MemberRole}, nil
	})

	repo := statsRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		time.Time,
		int,
	) (*domain.TeamStats, error) {
		t.Fatal("stats repository must not be called for member")
		return nil, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewStatsService(repo, members, log)

	got, err := svc.Get(ctx, actorID, teamID)

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
}

func TestStatsServiceGetNonMemberForbidden(t *testing.T) {
	ctx := context.Background()
	actorID, teamID := uuid.New(), uuid.New()

	members := statsMembershipRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*domain.TeamMember, error) {
		return nil, domain.ErrTeamMemberNotFound
	})

	repo := statsRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		time.Time,
		int,
	) (*domain.TeamStats, error) {
		t.Fatal("stats repository must not be called for non-member")
		return nil, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewStatsService(repo, members, log)

	got, err := svc.Get(ctx, actorID, teamID)

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
}

func TestStatsServiceGetInvalidIDs(t *testing.T) {
	for _, ids := range []struct {
		name    string
		actorID uuid.UUID
		teamID  uuid.UUID
	}{
		{name: "actor non-zero, team zero", actorID: uuid.New(), teamID: uuid.Nil},
		{name: "actor zero, team non-zero", actorID: uuid.Nil, teamID: uuid.New()},
	} {
		t.Run(ids.name, func(t *testing.T) {
			ctx := context.Background()
			actorID, teamID := ids.actorID, ids.teamID

			members := statsMembershipRepositoryFunc(func(
				context.Context,
				uuid.UUID,
				uuid.UUID,
			) (*domain.TeamMember, error) {
				t.Fatal("membership repository must not be called for zero IDs")
				return nil, nil
			})

			repo := statsRepositoryFunc(func(
				context.Context,
				uuid.UUID,
				time.Time,
				int,
			) (*domain.TeamStats, error) {
				t.Fatal("stats repository must not be called for zero IDs")
				return nil, nil
			})

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			svc := service.NewStatsService(repo, members, log)

			got, err := svc.Get(ctx, actorID, teamID)

			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if got != nil {
				t.Errorf("result = %v, want nil", got)
			}
		})
	}
}

func TestStatsServiceGetMembershipError(t *testing.T) {
	actorID, teamID := uuid.New(), uuid.New()
	wantErr := errors.New("database unavailable")

	members := statsMembershipRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*domain.TeamMember, error) {
		return nil, wantErr
	})

	repo := statsRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		time.Time,
		int,
	) (*domain.TeamStats, error) {
		t.Fatal("stats must not be requested after membership error")
		return nil, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewStatsService(repo, members, log)

	got, err := svc.Get(context.Background(), actorID, teamID)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
}

func TestStatsServiceGetStatsRepositoryError(t *testing.T) {
	ctx := context.Background()
	actorID, teamID := uuid.New(), uuid.New()
	wantErr := errors.New("stats database unavailable")
	membershipCalls, statsCalls := 0, 0

	members := statsMembershipRepositoryFunc(func(
		gotCtx context.Context,
		gotTeamID, gotUserID uuid.UUID,
	) (*domain.TeamMember, error) {
		membershipCalls++
		if gotCtx != ctx || gotTeamID != teamID || gotUserID != actorID {
			t.Error("membership received unexpected context or IDs")
		}
		return &domain.TeamMember{Role: domain.OwnerRole}, nil
	})

	repo := statsRepositoryFunc(func(
		gotCtx context.Context,
		gotTeamID uuid.UUID,
		since time.Time,
		limit int,
	) (*domain.TeamStats, error) {
		statsCalls++
		if membershipCalls != 1 {
			t.Error("stats requested before membership check")
		}
		if gotCtx != ctx || gotTeamID != teamID {
			t.Error("stats received unexpected context or team ID")
		}
		return nil, wantErr
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewStatsService(repo, members, log)

	got, err := svc.Get(ctx, actorID, teamID)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
	if membershipCalls != 1 || statsCalls != 1 {
		t.Errorf("calls: membership=%d stats=%d, want 1 each", membershipCalls, statsCalls)
	}
}

func TestStatsServiceGetUnknownRoleForbidden(t *testing.T) {
	ctx := context.Background()
	actorID, teamID := uuid.New(), uuid.New()
	membershipCalls := 0

	members := statsMembershipRepositoryFunc(func(
		gotCtx context.Context,
		gotTeamID, gotUserID uuid.UUID,
	) (*domain.TeamMember, error) {
		membershipCalls++
		if gotCtx != ctx || gotTeamID != teamID || gotUserID != actorID {
			t.Error("membership received unexpected context or IDs")
		}
		return &domain.TeamMember{Role: domain.TeamRole("unknown")}, nil
	})

	repo := statsRepositoryFunc(func(
		context.Context,
		uuid.UUID,
		time.Time,
		int,
	) (*domain.TeamStats, error) {
		t.Fatal("stats repository must not be called for unknown role")
		return nil, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewStatsService(repo, members, log)

	got, err := svc.Get(ctx, actorID, teamID)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
	if membershipCalls != 1 {
		t.Errorf("membership calls = %d, want 1", membershipCalls)
	}
}
