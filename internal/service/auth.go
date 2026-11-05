package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type UserRepository interface {
	Save(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type AuthService struct {
	cfg   config.Auth
	users UserRepository
	log   *slog.Logger
}

func NewAuthService(cfg config.Auth, repository UserRepository, log *slog.Logger) *AuthService {
	return &AuthService{
		cfg:   cfg,
		users: repository,
		log:   log,
	}
}

type RegisterInput struct {
	Email    string
	Name     string
	Password string
}

func (a *AuthService) Register(ctx context.Context, input RegisterInput) (*domain.User, error) {
	a.log.Debug("Auth service: Register user", "email", input.Email, "name", input.Name)
	email, err := emailNormalize(input.Email)
	if err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}
	name, err := nameNormalize(input.Name)
	if err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}
	password, err := hashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}
	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		Name:         name,
		PasswordHash: password,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
	err = a.users.Save(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}
	return user, nil
}

type LoginInput struct {
	Email    string
	Password string
}

func (a *AuthService) Login(ctx context.Context, input LoginInput) (string, error) {
	a.log.Debug("Auth service: Generate token", "email", input.Email)
	email, err := emailNormalize(input.Email)
	if err != nil {
		return "", fmt.Errorf("login: validate email: %w", err)
	}
	if err = checkPassword(input.Password); err != nil {
		return "", fmt.Errorf("login: validate password: %w", err)
	}
	user, err := a.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", domain.ErrInvalidCredentials
		}
		return "", fmt.Errorf("login: get user: %w", err)
	}
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", domain.ErrInvalidCredentials
		}
		return "", fmt.Errorf("login: compare password: %w", err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": user.ID.String(),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(a.cfg.JWTTtl).Unix(),
	})
	t, err := token.SignedString([]byte(a.cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("login: sign token: %w", err)
	}
	return t, nil
}

func (a *AuthService) ParseToken(tokenString string) (uuid.UUID, error) {
	a.log.Debug("Auth service: Parse token")
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte(a.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse token: parse: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return uuid.Nil, fmt.Errorf("parse token: empty claims")
	}
	id, err := claims.GetSubject()
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse token: wrong claims: %w", err)
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse token: wrong claims: %w", err)
	}
	return uid, nil
}

func checkPassword(password string) error {
	if len(password) == 0 || len(password) > 72 {
		return fmt.Errorf(
			"%w: password must contain between 1 and 72 bytes",
			domain.ErrInvalidInput,
		)
	}
	return nil
}

func hashPassword(password string) (string, error) {
	if err := checkPassword(password); err != nil {
		return "", err
	}
	passwordBytes := []byte(password)
	hashedBytes, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hashedBytes), nil
}
