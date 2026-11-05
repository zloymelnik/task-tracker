package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestCreateTeamTransactionCommit(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	teams := mysql.NewTeamRepository(db, log, 5*time.Second)
	members := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	s := service.NewTeamService(mysql.NewTxManager(db), teams, members, nil, log)
	creator := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	team, err := s.Create(ctx, creator, service.CreateTeamInput{Name: "  Команда  "})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	var stored struct {
		ID        []byte    `db:"id"`
		Name      string    `db:"name"`
		CreatedBy []byte    `db:"created_by"`
		CreatedAt time.Time `db:"created_at"`
	}
	if err := db.GetContext(ctx, &stored, "SELECT id, name, created_by, created_at FROM teams WHERE id = ?", team.ID[:]); err != nil {
		t.Fatalf("read committed team: %v", err)
	}
	id, err := uuid.FromBytes(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	createdBy, err := uuid.FromBytes(stored.CreatedBy)
	if err != nil {
		t.Fatal(err)
	}
	if id != team.ID || stored.Name != "Команда" || createdBy != creator || !stored.CreatedAt.Equal(team.CreatedAt) {
		t.Errorf("unexpected committed team: %+v", stored)
	}
	var member struct {
		UserID   []byte    `db:"user_id"`
		Role     string    `db:"role"`
		JoinedAt time.Time `db:"joined_at"`
	}
	if err := db.GetContext(ctx, &member, "SELECT user_id, role, joined_at FROM team_members WHERE team_id = ?", team.ID[:]); err != nil {
		t.Fatalf("read committed owner: %v", err)
	}
	userID, err := uuid.FromBytes(member.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if userID != creator || member.Role != string(domain.OwnerRole) || !member.JoinedAt.Equal(team.CreatedAt) {
		t.Errorf("unexpected committed owner: %+v", member)
	}
}

// Insert the real owner twice so the primary key rejects the second INSERT.
// Both the first owner INSERT and the preceding team INSERT must roll back.
type duplicateOwnerRepository struct {
	repo *mysql.TeamMemberRepository
}

func (r duplicateOwnerRepository) UpdateRole(ctx context.Context, teamID, userID uuid.UUID, role domain.TeamRole) error {
	return r.repo.UpdateRole(ctx, teamID, userID, role)
}

func (r duplicateOwnerRepository) GetByTeamAndUserID(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	return r.repo.GetByTeamAndUserID(ctx, teamID, userID)
}

func (r duplicateOwnerRepository) GetByTeamAndUserIDForUpdate(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	return r.repo.GetByTeamAndUserIDForUpdate(ctx, teamID, userID)
}

func (r duplicateOwnerRepository) Save(ctx context.Context, member *domain.TeamMember) error {
	if err := r.repo.Save(ctx, member); err != nil {
		return err
	}
	return r.repo.Save(ctx, member)
}

func TestCreateTeamTransactionRollback(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	teams := mysql.NewTeamRepository(db, log, 5*time.Second)
	members := duplicateOwnerRepository{repo: mysql.NewTeamMemberRepository(db, log, 5*time.Second)}
	s := service.NewTeamService(mysql.NewTxManager(db), teams, members, nil, log)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	team, err := s.Create(ctx, uuid.New(), service.CreateTeamInput{Name: "Must roll back"})
	if team != nil || !errors.Is(err, domain.ErrTeamMemberAlreadyExists) {
		t.Fatalf("Create()=(%v, %v), want nil and ErrTeamMemberAlreadyExists", team, err)
	}
	assertTeamTableCounts(t, db, 0, 0)
}

func TestTransactionManagerRollbackOnPanic(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamRepository(db, log, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const panicValue = "transaction callback panic"
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Errorf("panic=%v, want %q", got, panicValue)
			}
		}()
		err := mysql.NewTxManager(db).Do(ctx, func(txCtx context.Context) error {
			team := &domain.Team{ID: uuid.New(), Name: "Must roll back", CreatedBy: uuid.New(), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
			if err := repo.Save(txCtx, team); err != nil {
				t.Fatalf("Save(): %v", err)
			}
			panic(panicValue)
		})
		t.Fatalf("Do() returned %v instead of propagating panic", err)
	}()
	assertTeamTableCounts(t, db, 0, 0)
}

func TestTransactionManagerNestedCall(t *testing.T) {
	db := newTeamTestDB(t)
	m := mysql.NewTxManager(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var nestedErr error
	err := m.Do(ctx, func(txCtx context.Context) error {
		nestedErr = m.Do(txCtx, func(context.Context) error {
			t.Fatal("nested transaction callback must not run")
			return nil
		})
		return nestedErr
	})
	if nestedErr == nil || !errors.Is(err, nestedErr) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Do() error=%v, nested error=%v, want immediate nested-transaction rejection", err, nestedErr)
	}
}

func TestTransactionManagerCanceledContext(t *testing.T) {
	db := newTeamTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := mysql.NewTxManager(db).Do(ctx, func(context.Context) error {
		t.Fatal("canceled transaction must not invoke callback")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() error=%v, want context.Canceled", err)
	}
}

func assertTeamTableCounts(t *testing.T, db *sqlx.DB, teams, members int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tt := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM teams", teams},
		{"SELECT COUNT(*) FROM team_members", members},
	} {
		var got int
		if err := db.GetContext(ctx, &got, tt.query); err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if got != tt.want {
			t.Errorf("%s = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func newTeamTestDB(t *testing.T) *sqlx.DB {
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
	db, err := sqlx.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	// One connection keeps temporary tables private and available across transactions.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, migration := range []struct{ file, table string }{
		{"000002_teams.up.sql", "teams"},
		{"000003_team_members.up.sql", "team_members"},
	} {
		schema, err := os.ReadFile("../../../migrations/" + migration.file)
		if err != nil {
			t.Fatal(err)
		}
		prefix := "CREATE TABLE " + migration.table + " ("
		if !strings.HasPrefix(string(schema), prefix) {
			t.Fatalf("unexpected migration prefix in %s", migration.file)
		}
		// MySQL temporary tables do not support foreign keys. These tests check
		// transaction atomicity and row mapping, not foreign-key enforcement.
		var lines []string
		for _, line := range strings.Split(string(schema), "\n") {
			if !strings.Contains(line, "FOREIGN KEY") {
				lines = append(lines, line)
			}
		}
		query := strings.Join(lines, "\n")
		query = strings.Replace(query, prefix, "CREATE TEMPORARY TABLE "+migration.table+" (", 1)
		query = strings.ReplaceAll(query, ",\n)", "\n)")
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("create temporary %s: %v", migration.table, err)
		}
	}
	return db
}
