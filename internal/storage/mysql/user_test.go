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
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

// TestUserRepository requires MYSQL_TEST_DSN, for example:
// MYSQL_TEST_DSN='app:password@tcp(mysql:3306)/team_tasks' go test -race ./internal/storage/mysql -v
// Each subtest uses a connection-local temporary users table, leaving existing data intact.
func TestUserRepository(t *testing.T) {
	t.Run("save and read", func(t *testing.T) {
		repo := newTestUserRepository(t)
		want := testUser()
		if err := repo.Save(context.Background(), want); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
		got, err := repo.GetByEmail(context.Background(), want.Email)
		if err != nil {
			t.Fatalf("GetByEmail() error: %v", err)
		}
		if got == nil {
			t.Fatal("GetByEmail() returned nil user")
		}
		if got.ID != want.ID || got.Email != want.Email || got.Name != want.Name ||
			got.PasswordHash != want.PasswordHash || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("GetByEmail() = %+v, want %+v", got, want)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		repo := newTestUserRepository(t)
		got, err := repo.GetByEmail(context.Background(), testUser().Email)
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("GetByEmail() error = %v, want ErrUserNotFound", err)
		}
		if got != nil {
			t.Errorf("GetByEmail() user = %+v, want nil", got)
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		repo := newTestUserRepository(t)
		first := testUser()
		if err := repo.Save(context.Background(), first); err != nil {
			t.Fatalf("Save(first) error: %v", err)
		}
		second := testUser()
		second.Email = first.Email
		if err := repo.Save(context.Background(), second); !errors.Is(err, domain.ErrEmailAlreadyExists) {
			t.Fatalf("Save(duplicate email) error = %v, want ErrEmailAlreadyExists", err)
		}
		got, err := repo.GetByEmail(context.Background(), first.Email)
		if err != nil || got == nil || got.ID != first.ID {
			t.Fatalf("original user changed: user=%+v error=%v", got, err)
		}
	})

	t.Run("duplicate UUID is not duplicate email", func(t *testing.T) {
		repo := newTestUserRepository(t)
		first := testUser()
		if err := repo.Save(context.Background(), first); err != nil {
			t.Fatalf("Save(first) error: %v", err)
		}
		second := testUser()
		second.ID = first.ID
		err := repo.Save(context.Background(), second)
		if err == nil || errors.Is(err, domain.ErrEmailAlreadyExists) {
			t.Fatalf("Save(duplicate UUID) error = %v, want storage error", err)
		}
		var mysqlErr *mysqldriver.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
			t.Fatalf("Save(duplicate UUID) error = %v, want wrapped MySQL error 1062", err)
		}
		if _, err := repo.GetByEmail(context.Background(), second.Email); !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("duplicate UUID inserted a user: error = %v", err)
		}
	})

	t.Run("canceled save", func(t *testing.T) {
		repo := newTestUserRepository(t)
		u := testUser()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := repo.Save(ctx, u); !errors.Is(err, context.Canceled) {
			t.Fatalf("Save() error = %v, want context.Canceled", err)
		}
		if _, err := repo.GetByEmail(context.Background(), u.Email); !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("canceled Save inserted a user: error = %v", err)
		}
	})

	t.Run("canceled read", func(t *testing.T) {
		repo := newTestUserRepository(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := repo.GetByEmail(ctx, testUser().Email)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetByEmail() error = %v, want context.Canceled", err)
		}
		if got != nil {
			t.Errorf("GetByEmail() user = %+v, want nil", got)
		}
	})
}

func newTestUserRepository(t *testing.T) *mysql.UserRepository {
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
	// Temporary tables belong to one connection. Keep the same connection for all queries.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	schema, err := os.ReadFile("../../../migrations/000001_create_users_table.up.sql")
	if err != nil {
		t.Fatalf("read users migration: %v", err)
	}
	const create = "CREATE TABLE users"
	if !strings.HasPrefix(string(schema), create) {
		t.Fatal("users migration must start with CREATE TABLE users for temporary-table isolation")
	}
	query := strings.Replace(string(schema), create, "CREATE TEMPORARY TABLE users", 1)
	if _, err := db.ExecContext(ctx, query); err != nil {
		t.Fatalf("create temporary users table: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return mysql.NewUserRepository(db, log, 5*time.Second)
}

func testUser() *domain.User {
	id := uuid.New()
	return &domain.User{
		ID:           id,
		Email:        id.String() + "@example.test",
		Name:         "Тестовый пользователь",
		PasswordHash: "test-password-hash",
		CreatedAt:    time.Date(2026, time.October, 30, 10, 20, 30, 123000000, time.UTC),
	}
}
