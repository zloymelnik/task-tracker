package mysql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestTeamStats(t *testing.T) {
	t.Run("empty data check", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		usr := testUser("empty")
		if err := userRepo.Save(ctx, usr); err != nil {
			t.Fatalf("save user: %v", err)
		}
		team := testTeam("empty", usr.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}
		member := testTeamMember(usr, team, domain.OwnerRole)
		if err := memberRepo.Save(ctx, member); err != nil {
			t.Fatalf("save member: %v", err)
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("Get stats error %v", err)
		}
		if stats == nil {
			t.Fatalf("Stats must not be nil")
		}
		if stats.TeamID != team.ID {
			t.Fatalf("Stats teamId = %s, needed = %s", stats.TeamID.String(), team.ID.String())
		}
		if stats.TasksByStatus.Todo != 0 {
			t.Errorf("Todo = %d, want 0", stats.TasksByStatus.Todo)
		}
		if stats.TasksByStatus.InProgress != 0 {
			t.Errorf("InProgress = %d, want 0", stats.TasksByStatus.InProgress)
		}
		if stats.TasksByStatus.Done != 0 {
			t.Errorf("Done = %d, want 0", stats.TasksByStatus.Done)
		}
		if stats.CommentsCount != 0 {
			t.Errorf("CommentsCount = %d, want 0", stats.CommentsCount)
		}
		if stats.AverageCloseSeconds != nil {
			t.Fatalf("Stats AverageCloseSeconds = %f, want nil", *stats.AverageCloseSeconds)
		}
		if len(stats.TopAssignees) != 0 || stats.TopAssignees == nil {
			t.Fatalf("TopAssignees must be empty, not be nil or filled")
		}
	})
	t.Run("statuses check", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		usr := testUser("statuses")
		if err := userRepo.Save(ctx, usr); err != nil {
			t.Fatalf("save user: %v", err)
		}
		team := testTeam("statuses", usr.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}

		createdAt := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
		statuses := []domain.TaskStatus{
			domain.Todo, domain.Todo,
			domain.InProgress,
			domain.Done, domain.Done, domain.Done,
		}
		for _, status := range statuses {
			task := &domain.Task{
				ID:          uuid.New(),
				TeamID:      team.ID,
				CreatedBy:   usr.ID,
				Title:       "Task with status " + string(status),
				Description: "Test task for status counts",
				Status:      status,
				CreatedAt:   createdAt,
				UpdatedAt:   createdAt,
				Version:     1,
			}
			if status == domain.Done {
				closedAt := createdAt.Add(time.Hour)
				task.ClosedAt = &closedAt
				task.UpdatedAt = closedAt
			}
			if err := taskRepo.Save(ctx, task); err != nil {
				t.Fatalf("save task with status %s: %v", status, err)
			}
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if stats.TasksByStatus.Todo != 2 {
			t.Errorf("Todo = %d, want 2", stats.TasksByStatus.Todo)
		}
		if stats.TasksByStatus.InProgress != 1 {
			t.Errorf("InProgress = %d, want 1", stats.TasksByStatus.InProgress)
		}
		if stats.TasksByStatus.Done != 3 {
			t.Errorf("Done = %d, want 3", stats.TasksByStatus.Done)
		}
	})
	t.Run("period check", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		usr := testUser("period")
		if err := userRepo.Save(ctx, usr); err != nil {
			t.Fatalf("save user: %v", err)
		}
		team := testTeam("period", usr.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		beforeSince := since.Add(-time.Millisecond)
		afterSince := since.Add(time.Millisecond)
		createdAt := since.Add(-time.Millisecond * 10)

		for _, date := range []time.Time{beforeSince, since, afterSince} {
			task := &domain.Task{
				ID:          uuid.New(),
				TeamID:      team.ID,
				CreatedBy:   usr.ID,
				AssigneeID:  &usr.ID,
				Title:       "Task with since " + date.Format("2006-01-02 15:04:05.000"),
				Description: "Test task for period check",
				Status:      domain.Done,
				CreatedAt:   createdAt,
				UpdatedAt:   date,
				ClosedAt:    &date,
				Version:     1,
			}

			if err := taskRepo.Save(ctx, task); err != nil {
				t.Fatalf("save task with closedAt %s: %v", date.Format("2006-01-02 15:04:05.000"), err)
			}
		}
		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if len(stats.TopAssignees) != 1 {
			t.Fatalf("TopAssignees must not be %d, want 1", len(stats.TopAssignees))
		}
		if stats.TopAssignees[0].UserID != usr.ID {
			t.Fatalf("TopAssignees's user must not be %s, want %s", stats.TopAssignees[0].UserID, usr.ID)
		}
		if stats.TopAssignees[0].ClosedTasks != 2 {
			t.Errorf("ClosedTasks = %d, want 2", stats.TopAssignees[0].ClosedTasks)
		}
		if stats.TasksByStatus.Done != 3 {
			t.Fatalf("Done closed tasks must not be %d, want 3", stats.TasksByStatus.Done)
		}
	})
	t.Run("top 3 check", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assignees := []struct {
			user  *domain.User
			count int
		}{
			{user: testUser("A"), count: 4},
			{user: testUser("B"), count: 3},
			{user: testUser("C"), count: 2},
			{user: testUser("D"), count: 1},
		}
		for _, assignee := range assignees {
			if err := userRepo.Save(ctx, assignee.user); err != nil {
				t.Fatalf("save user %s: %v", assignee.user.Name, err)
			}
		}
		owner := assignees[0].user
		team := testTeam("top 3", owner.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}
		for i, assignee := range assignees {
			role := domain.MemberRole
			if i == 0 {
				role = domain.OwnerRole
			}
			if err := memberRepo.Save(ctx, testTeamMember(assignee.user, team, role)); err != nil {
				t.Fatalf("save member %s: %v", assignee.user.Name, err)
			}
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		createdAt := since.Add(time.Hour)
		closedAt := createdAt.Add(time.Hour)
		// Insert tasks in a different order from the expected ranking.
		for _, index := range []int{3, 1, 0, 2} {
			assignee := assignees[index]
			for i := 0; i < assignee.count; i++ {
				task := &domain.Task{
					ID:          uuid.New(),
					TeamID:      team.ID,
					CreatedBy:   owner.ID,
					AssigneeID:  &assignee.user.ID,
					Title:       "Closed task for " + assignee.user.Name,
					Description: "Test task for top 3 ranking",
					Status:      domain.Done,
					CreatedAt:   createdAt,
					UpdatedAt:   closedAt,
					ClosedAt:    &closedAt,
					Version:     1,
				}
				if err := taskRepo.Save(ctx, task); err != nil {
					t.Fatalf("save task for %s: %v", assignee.user.Name, err)
				}
			}
		}

		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if len(stats.TopAssignees) != 3 {
			t.Fatalf("TopAssignees length = %d, want 3", len(stats.TopAssignees))
		}
		for i, got := range stats.TopAssignees {
			want := assignees[i]
			if got.UserID != want.user.ID {
				t.Errorf("TopAssignees[%d].UserID = %s, want %s", i, got.UserID, want.user.ID)
			}
			if got.Name != want.user.Name {
				t.Errorf("TopAssignees[%d].Name = %q, want %q", i, got.Name, want.user.Name)
			}
			if got.ClosedTasks != want.count {
				t.Errorf("TopAssignees[%d].ClosedTasks = %d, want %d", i, got.ClosedTasks, want.count)
			}
		}
		if stats.TasksByStatus.Done != 10 {
			t.Errorf("Done = %d, want 10", stats.TasksByStatus.Done)
		}
	})
	t.Run("comments check", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)
		commentsRepo := mysql.NewTaskCommentRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		usr := testUser("comments")
		if err := userRepo.Save(ctx, usr); err != nil {
			t.Fatalf("save user: %v", err)
		}
		team := testTeam("comments", usr.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}
		member := testTeamMember(usr, team, domain.OwnerRole)
		if err := memberRepo.Save(ctx, member); err != nil {
			t.Fatalf("save member: %v", err)
		}

		for _, status := range []domain.TaskStatus{domain.Todo, domain.InProgress} {
			createdAt := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
			task := &domain.Task{
				ID:          uuid.New(),
				TeamID:      team.ID,
				CreatedBy:   usr.ID,
				Title:       "Task with comments, status: " + string(status),
				Description: "Test task for comments check",
				Status:      status,
				CreatedAt:   createdAt,
				UpdatedAt:   createdAt,
				Version:     1,
			}
			if err := taskRepo.Save(ctx, task); err != nil {
				t.Fatalf("save task with status %s: %v", status, err)
			}
			if task.Status == domain.Todo {
				for i := 0; i < 3; i++ {
					comment := &domain.TaskComment{
						ID:        uuid.New(),
						TaskID:    task.ID,
						CreatedBy: usr.ID,
						Body:      "Task comment #" + strconv.Itoa(i),
						CreatedAt: createdAt,
					}
					if err := commentsRepo.Save(ctx, comment); err != nil {
						t.Fatalf("save comment: %v", err)
					}
				}
			}
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if stats.CommentsCount != 3 {
			t.Errorf("stats.CommentsCount = %d, want 3", stats.CommentsCount)
		}
		if stats.TasksByStatus.Todo != 1 {
			t.Errorf("stats.TasksByStatus.Todo = %d, want 1", stats.TasksByStatus.Todo)
		}
		if stats.TasksByStatus.InProgress != 1 {
			t.Errorf("stats.TasksByStatus.InProgress = %d, want 1", stats.TasksByStatus.InProgress)
		}
		if stats.TasksByStatus.Done != 0 {
			t.Errorf("stats.TasksByStatus.Done = %d, want 0", stats.TasksByStatus.Done)
		}
	})
	t.Run("average close time", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		usr := testUser("average close time")
		if err := userRepo.Save(ctx, usr); err != nil {
			t.Fatalf("save user: %v", err)
		}
		team := testTeam("average close time", usr.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}
		member := testTeamMember(usr, team, domain.OwnerRole)
		if err := memberRepo.Save(ctx, member); err != nil {
			t.Fatalf("save member: %v", err)
		}

		now := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		for _, data := range []struct {
			status domain.TaskStatus
			start  time.Time
			end    time.Time
		}{
			{
				status: domain.Done,
				start:  now,
				end:    now.Add(2500 * time.Millisecond),
			},
			{
				status: domain.Done,
				start:  now,
				end:    now.Add(1500 * time.Millisecond),
			},
			{
				status: domain.InProgress,
				start:  now,
				end:    now,
			},
		} {
			task := &domain.Task{
				ID:          uuid.New(),
				TeamID:      team.ID,
				CreatedBy:   usr.ID,
				Title:       "Task with average close time, status: " + string(data.status),
				Description: "Test task for average close time check",
				Status:      data.status,
				CreatedAt:   data.start,
				UpdatedAt:   data.end,
				Version:     1,
			}
			if data.status == domain.Done {
				task.ClosedAt = &data.end
			}
			if err := taskRepo.Save(ctx, task); err != nil {
				t.Fatalf("save task with status %s: %v", data.status, err)
			}
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if stats.AverageCloseSeconds == nil {
			t.Fatal("stats.AverageCloseSeconds must not be nil")
		}
		if *stats.AverageCloseSeconds != 2.0 {
			t.Errorf("stats.AverageCloseSeconds = %f, want 2.0", *stats.AverageCloseSeconds)
		}
	})
	t.Run("team isolation", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)
		commentRepo := mysql.NewTaskCommentRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		userA, userB := testUser("A"), testUser("B")
		teamA, teamB := testTeam("A", userA.ID), testTeam("B", userB.ID)
		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		createdAt := since.Add(time.Hour)

		for _, data := range []struct {
			user       *domain.User
			team       *domain.Team
			todo       int
			done       int
			comments   int
			closeAfter time.Duration
		}{
			{user: userA, team: teamA, todo: 1, done: 1, comments: 1, closeAfter: 2 * time.Second},
			{user: userB, team: teamB, todo: 2, done: 3, comments: 4, closeAfter: 10 * time.Second},
		} {
			if err := userRepo.Save(ctx, data.user); err != nil {
				t.Fatalf("save user %s: %v", data.user.Name, err)
			}
			if err := teamRepo.Save(ctx, data.team); err != nil {
				t.Fatalf("save team %s: %v", data.team.Name, err)
			}
			if err := memberRepo.Save(ctx, testTeamMember(data.user, data.team, domain.OwnerRole)); err != nil {
				t.Fatalf("save member: %v", err)
			}
			for i := 0; i < data.todo+data.done; i++ {
				task := &domain.Task{
					ID:          uuid.New(),
					TeamID:      data.team.ID,
					CreatedBy:   data.user.ID,
					AssigneeID:  &data.user.ID,
					Title:       "Task for " + data.team.Name,
					Description: "Test task for team isolation",
					Status:      domain.Todo,
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt,
					Version:     1,
				}
				if i >= data.todo {
					closedAt := createdAt.Add(data.closeAfter)
					task.Status = domain.Done
					task.ClosedAt = &closedAt
					task.UpdatedAt = closedAt
				}
				if err := taskRepo.Save(ctx, task); err != nil {
					t.Fatalf("save task for team %s: %v", data.team.Name, err)
				}
				// Attach all comments to the first task of each team.
				if i == 0 {
					for j := 0; j < data.comments; j++ {
						comment := &domain.TaskComment{
							ID:        uuid.New(),
							TaskID:    task.ID,
							CreatedBy: data.user.ID,
							Body:      "Comment for " + data.team.Name,
							CreatedAt: createdAt,
						}
						if err := commentRepo.Save(ctx, comment); err != nil {
							t.Fatalf("save comment for team %s: %v", data.team.Name, err)
						}
					}
				}
			}
		}

		stats, err := statsRepo.Get(ctx, teamA.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if stats.TeamID != teamA.ID {
			t.Errorf("TeamID = %s, want %s", stats.TeamID, teamA.ID)
		}
		wantCounts := domain.TaskStatusCounts{Todo: 1, InProgress: 0, Done: 1}
		if stats.TasksByStatus != wantCounts {
			t.Errorf("TasksByStatus = %+v, want %+v", stats.TasksByStatus, wantCounts)
		}
		if stats.CommentsCount != 1 {
			t.Errorf("CommentsCount = %d, want 1", stats.CommentsCount)
		}
		if stats.AverageCloseSeconds == nil {
			t.Fatal("AverageCloseSeconds must not be nil")
		}
		if *stats.AverageCloseSeconds != 2.0 {
			t.Errorf("AverageCloseSeconds = %f, want 2.0", *stats.AverageCloseSeconds)
		}
		if len(stats.TopAssignees) != 1 {
			t.Fatalf("TopAssignees length = %d, want 1", len(stats.TopAssignees))
		}
		wantAssignee := domain.AssigneeStats{UserID: userA.ID, Name: userA.Name, ClosedTasks: 1}
		if stats.TopAssignees[0] != wantAssignee {
			t.Errorf("TopAssignees[0] = %+v, want %+v", stats.TopAssignees[0], wantAssignee)
		}
	})
	t.Run("top ties", func(t *testing.T) {
		db, statsRepo := newTestTeamStatsRepository(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		timeout := 5 * time.Second
		userRepo := mysql.NewUserRepository(db, log, timeout)
		teamRepo := mysql.NewTeamRepository(db, log, timeout)
		memberRepo := mysql.NewTeamMemberRepository(db, log, timeout)
		taskRepo := mysql.NewTaskRepository(db, log, timeout)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ids := []uuid.UUID{
			uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			uuid.MustParse("00000000-0000-0000-0000-000000000003"),
			uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		}
		users := make([]*domain.User, len(ids))
		for i, id := range ids {
			usr := testUser("ties " + strconv.Itoa(i))
			usr.ID = id
			users[i] = usr
			if err := userRepo.Save(ctx, usr); err != nil {
				t.Fatalf("save user %s: %v", id, err)
			}
		}
		owner := users[0]
		team := testTeam("top ties", owner.ID)
		if err := teamRepo.Save(ctx, team); err != nil {
			t.Fatalf("save team: %v", err)
		}
		for i, usr := range users {
			role := domain.MemberRole
			if i == 0 {
				role = domain.OwnerRole
			}
			if err := memberRepo.Save(ctx, testTeamMember(usr, team, role)); err != nil {
				t.Fatalf("save member %s: %v", usr.ID, err)
			}
		}

		since := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
		createdAt := since.Add(time.Hour)
		closedAt := createdAt.Add(time.Second)
		for i := len(users) - 1; i >= 0; i-- {
			// Fixed task IDs make primary-key order oppose the expected ranking.
			task := &domain.Task{
				ID:          ids[len(ids)-1-i],
				TeamID:      team.ID,
				CreatedBy:   owner.ID,
				AssigneeID:  &users[i].ID,
				Title:       "Task for " + users[i].Name,
				Description: "Test equal counts at the top 3 boundary",
				Status:      domain.Done,
				CreatedAt:   createdAt,
				UpdatedAt:   closedAt,
				ClosedAt:    &closedAt,
				Version:     1,
			}
			if err := taskRepo.Save(ctx, task); err != nil {
				t.Fatalf("save task for %s: %v", users[i].ID, err)
			}
		}

		stats, err := statsRepo.Get(ctx, team.ID, since, 3)
		if err != nil {
			t.Fatalf("get stats: %v", err)
		}
		if stats == nil {
			t.Fatal("stats must not be nil")
		}
		if len(stats.TopAssignees) != 3 {
			t.Fatalf("TopAssignees length = %d, want 3", len(stats.TopAssignees))
		}
		for i, got := range stats.TopAssignees {
			want := domain.AssigneeStats{UserID: ids[i], Name: users[i].Name, ClosedTasks: 1}
			if got != want {
				t.Errorf("TopAssignees[%d] = %+v, want %+v", i, got, want)
			}
		}
		if stats.TasksByStatus.Done != 4 {
			t.Errorf("Done = %d, want 4", stats.TasksByStatus.Done)
		}
	})
}

// Rewrite table and constraint names in test SQL, including repository queries.
// Bound parameter values remain unchanged.
type statsTestConnector struct {
	driver.Connector
	rewrite func(string) string
}

func (c statsTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &statsTestConn{Conn: conn, rewrite: c.rewrite}, nil
}

type statsTestConn struct {
	driver.Conn
	rewrite func(string) string
}

func (c *statsTestConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(c.rewrite(query))
}
func (c *statsTestConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *statsTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if exec, ok := c.Conn.(driver.ExecerContext); ok {
		return exec.ExecContext(ctx, c.rewrite(query), args)
	}
	return nil, driver.ErrSkip
}
func (c *statsTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, c.rewrite(query), args)
	}
	return nil, driver.ErrSkip
}

func newTestTeamStatsRepository(t *testing.T) (*sqlx.DB, *mysql.TeamStatsRepository) {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run MySQL integration tests")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MYSQL_TEST_DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		t.Fatalf("create test database connector: %v", err)
	}
	// Keep generated table and constraint names within MySQL's 64-character limit.
	prefix := "t" + strings.ReplaceAll(uuid.NewString(), "-", "") + "_"
	identifiers := regexp.MustCompile(`\b(task_comments|team_members|tasks|teams|users|(?:fk|idx|uq)_[A-Za-z0-9_]+)\b`)
	isolated := statsTestConnector{Connector: connector, rewrite: func(query string) string {
		return identifiers.ReplaceAllStringFunc(query, func(name string) string { return prefix + name })
	}}
	db := sqlx.NewDb(sql.OpenDB(isolated), "mysql")
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	var created []string
	t.Cleanup(func() {
		// Use a separate connection so cleanup also works if a test closes db.
		cleanupDB := sqlx.NewDb(sql.OpenDB(isolated), "mysql")
		defer func() {
			if err := cleanupDB.Close(); err != nil {
				t.Errorf("close cleanup database: %v", err)
			}
		}()
		defer func() {
			if err := db.Close(); err != nil {
				t.Errorf("close test database: %v", err)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := cleanupDB.ExecContext(ctx, "DROP TABLE "+created[i]); err != nil {
				t.Errorf("drop isolated %s: %v", created[i], err)
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	for _, migration := range []struct{ file, table string }{
		{"000001_create_users_table.up.sql", "users"},
		{"000002_teams.up.sql", "teams"},
		{"000003_team_members.up.sql", "team_members"},
		{"000004_tasks.up.sql", "tasks"},
		{"000006_task_comments.up.sql", "task_comments"},
	} {
		schema, err := os.ReadFile("../../../migrations/" + migration.file)
		if err != nil {
			t.Fatalf("read %s migration: %v", migration.table, err)
		}
		if _, err := db.ExecContext(ctx, string(schema)); err != nil {
			t.Fatalf("create isolated %s: %v", migration.table, err)
		}
		created = append(created, migration.table)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return db, mysql.NewTeamStatsRepository(db, log, 5*time.Second)
}

func testUser(prefix string) *domain.User {
	id := uuid.New()
	return &domain.User{
		ID:           id,
		Email:        id.String() + "@example.test",
		Name:         prefix + "Тестовый пользователь",
		PasswordHash: "test-password-hash",
		CreatedAt:    time.Date(2026, time.October, 30, 10, 20, 30, 123000000, time.UTC),
	}
}

func testTeam(prefix string, uid uuid.UUID) *domain.Team {
	id := uuid.New()
	return &domain.Team{
		ID:        id,
		Name:      prefix + "Тестовая группа пользователя: " + uid.String(),
		CreatedAt: time.Date(2026, time.October, 30, 10, 20, 30, 123000000, time.UTC),
		CreatedBy: uid,
	}
}

func testTeamMember(user *domain.User, team *domain.Team, role domain.TeamRole) *domain.TeamMember {
	return &domain.TeamMember{
		TeamID:   team.ID,
		UserID:   user.ID,
		Role:     role,
		JoinedAt: time.Date(2026, time.October, 30, 10, 20, 30, 123000000, time.UTC),
	}
}
