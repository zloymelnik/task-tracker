package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type UserRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewUserRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *UserRepository {
	return &UserRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (u *UserRepository) Save(ctx context.Context, user *domain.User) error {
	u.log.Debug("MySQL: User: Save user data", "email", user.Email, "name", user.Name, "id", user.ID)
	ctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	_, err := u.db.ExecContext(ctx,
		"INSERT INTO users (id, email, name, password_hash, created_at) VALUES (?, ?, ?, ?, ?)",
		uuidToBytes(user.ID), user.Email, user.Name, user.PasswordHash, user.CreatedAt,
	)
	if err != nil {
		if isErrEmailAlreadyExists(err) {
			return domain.ErrEmailAlreadyExists
		}
		return fmt.Errorf("MySQL: User: Failed to save user data: %w", err)
	}

	u.log.Debug("MySQL: User: User data saved successfully")
	return nil
}

type rawUser struct {
	ID           []byte    `db:"id"`
	Email        string    `db:"email"`
	Name         string    `db:"name"`
	PasswordHash string    `db:"password_hash"`
	CreatedAt    time.Time `db:"created_at"`
}

func rawToUser(r *rawUser) (*domain.User, error) {
	id, err := bytesToUUID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: User: Wrong parse user id: %w", err)
	}
	return &domain.User{
		ID:           id,
		Email:        r.Email,
		Name:         r.Name,
		PasswordHash: r.PasswordHash,
		CreatedAt:    r.CreatedAt,
	}, nil
}

func (u *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	u.log.Debug("MySQL: User: Get by email", "email", email)
	ctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	var raw rawUser
	err := u.db.GetContext(ctx, &raw, `SELECT id, email, name, password_hash, created_at FROM users WHERE email = ? LIMIT 1`, email)
	if err != nil {
		if isErrUserNotFound(err) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("MySQL: User: Failed to get user data: %w", err)
	}
	user, err := rawToUser(&raw)
	if err != nil {
		return nil, fmt.Errorf("MySQL: User: Failed to parse user row: %w", err)
	}
	u.log.Debug("MySQL: User: User data retrieved successfully", "email", email)
	return user, nil
}
