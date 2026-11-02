package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"golang.org/x/crypto/bcrypt"
)

type userRepositoryStub struct {
	save       func(context.Context, *domain.User) error
	getByEmail func(context.Context, string) (*domain.User, error)
}

func (r userRepositoryStub) Save(ctx context.Context, user *domain.User) error {
	return r.save(ctx, user)
}

func (r userRepositoryStub) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if r.getByEmail == nil {
		panic("unexpected GetByEmail call in repository stub")
	}
	return r.getByEmail(ctx, email)
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name      string
		input     service.RegisterInput
		wantEmail string
		wantName  string
	}{
		{
			name: "normalizes email and name without changing password",
			input: service.RegisterInput{
				Email: " \tUser@Example.COM\n", Name: "  Иван Петров \t", Password: "  пароль с пробелами  ",
			},
			wantEmail: "user@example.com", wantName: "Иван Петров",
		},
		{
			name: "255 character Unicode name",
			input: service.RegisterInput{
				Email: "user@example.com", Name: strings.Repeat("я", 255), Password: "password",
			},
			wantEmail: "user@example.com", wantName: strings.Repeat("я", 255),
		},
		{
			name: "255 byte email",
			input: service.RegisterInput{
				Email: maxLengthEmail(), Name: "User", Password: "password",
			},
			wantEmail: maxLengthEmail(), wantName: "User",
		},
		{
			name: "72 byte Unicode password",
			input: service.RegisterInput{
				Email: "user@example.com", Name: "User", Password: strings.Repeat("я", 36),
			},
			wantEmail: "user@example.com", wantName: "User",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var saved domain.User
			repo := userRepositoryStub{save: func(gotCtx context.Context, user *domain.User) error {
				calls++
				if gotCtx != ctx {
					t.Error("Save received a different context")
				}
				saved = *user
				return nil
			}}
			auth := service.NewAuth(config.Auth{}, repo, testLogger())
			before := time.Now().UTC().Truncate(time.Millisecond)
			got, err := auth.Register(ctx, tt.input)
			after := time.Now().UTC()
			if err != nil {
				t.Fatalf("Register() error: %v", err)
			}
			if got == nil || calls != 1 {
				t.Fatalf("Register() user=%v, Save calls=%d; want user and one call", got, calls)
			}
			if *got != saved {
				t.Error("returned user differs from saved user")
			}
			if got.Email != tt.wantEmail || got.Name != tt.wantName {
				t.Errorf("email/name = %q/%q, want %q/%q", got.Email, got.Name, tt.wantEmail, tt.wantName)
			}
			if got.ID == uuid.Nil || got.ID.Version() != 4 {
				t.Errorf("ID = %s, want nonzero UUID v4", got.ID)
			}
			if got.CreatedAt.Before(before) || got.CreatedAt.After(after) ||
				got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond()%int(time.Millisecond) != 0 {
				t.Errorf("CreatedAt = %v, want current UTC time with millisecond precision", got.CreatedAt)
			}
			if got.PasswordHash == tt.input.Password {
				t.Fatal("password was saved without hashing")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte(tt.input.Password)); err != nil {
				t.Errorf("saved bcrypt hash does not match original password: %v", err)
			}
		})
	}
}

func TestRegisterInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input service.RegisterInput
	}{
		{"empty email", service.RegisterInput{Email: "", Name: "User", Password: "password"}},
		{"blank email", service.RegisterInput{Email: " \t\n", Name: "User", Password: "password"}},
		{"invalid email", service.RegisterInput{Email: "not-an-email", Name: "User", Password: "password"}},
		{"email with display name", service.RegisterInput{Email: "User <user@example.com>", Name: "User", Password: "password"}},
		{"email too long", service.RegisterInput{Email: maxLengthEmail() + "a", Name: "User", Password: "password"}},
		{"empty name", service.RegisterInput{Email: "user@example.com", Name: "", Password: "password"}},
		{"blank name", service.RegisterInput{Email: "user@example.com", Name: " \t\n", Password: "password"}},
		{"name too long", service.RegisterInput{Email: "user@example.com", Name: strings.Repeat("я", 256), Password: "password"}},
		{"empty password", service.RegisterInput{Email: "user@example.com", Name: "User", Password: ""}},
		{"73 byte password", service.RegisterInput{Email: "user@example.com", Name: "User", Password: strings.Repeat("a", 73)}},
		{"74 byte Unicode password", service.RegisterInput{Email: "user@example.com", Name: "User", Password: strings.Repeat("я", 37)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := userRepositoryStub{save: func(context.Context, *domain.User) error {
				t.Fatal("Save must not be called for invalid input")
				return nil
			}}
			got, err := service.NewAuth(config.Auth{}, repo, testLogger()).Register(context.Background(), tt.input)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Register() error = %v, want ErrInvalidInput", err)
			}
			if got != nil {
				t.Error("Register() returned a user for invalid input")
			}
		})
	}
}

func TestRegisterRepositoryErrors(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	for _, want := range []error{domain.ErrEmailAlreadyExists, storageErr} {
		t.Run(want.Error(), func(t *testing.T) {
			calls := 0
			repo := userRepositoryStub{save: func(context.Context, *domain.User) error {
				calls++
				return fmt.Errorf("save user: %w", want)
			}}
			got, err := service.NewAuth(config.Auth{}, repo, testLogger()).Register(context.Background(), validRegisterInput())
			if !errors.Is(err, want) {
				t.Errorf("Register() error = %v, want wrapping %v", err, want)
			}
			if got != nil || calls != 1 {
				t.Errorf("Register() user=%v, Save calls=%d; want nil and one call", got, calls)
			}
		})
	}
}

func TestRegisterCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := userRepositoryStub{save: func(ctx context.Context, _ *domain.User) error {
		return ctx.Err()
	}}
	got, err := service.NewAuth(config.Auth{}, repo, testLogger()).Register(ctx, validRegisterInput())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Register() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Error("Register() returned a user after cancellation")
	}
}

func TestLogin(t *testing.T) {
	cfg := testAuthConfig()
	password := "  пароль с пробелами  "
	user := domain.User{ID: uuid.New(), PasswordHash: testPasswordHash(t, password)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	repo := userRepositoryStub{getByEmail: func(gotCtx context.Context, email string) (*domain.User, error) {
		calls++
		if gotCtx != ctx || email != "user@example.com" {
			t.Errorf("GetByEmail received wrong context or email: %q", email)
		}
		return &user, nil
	}}
	auth := service.NewAuth(cfg, repo, testLogger())
	before := time.Now().Truncate(time.Second)
	signed, err := auth.Login(ctx, service.LoginInput{Email: " \tUser@Example.COM\n", Password: password})
	after := time.Now().Truncate(time.Second)
	if err != nil {
		t.Fatalf("Login() error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("GetByEmail calls=%d, want 1", calls)
	}
	// Validate independently before checking compatibility with ParseToken.
	claims := &jwt.RegisteredClaims{}
	_, err = jwt.ParseWithClaims(signed, claims, func(*jwt.Token) (any, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		t.Fatalf("Login returned invalid token: %v", err)
	}
	if claims.Subject != user.ID.String() {
		t.Errorf("subject=%q, want %s", claims.Subject, user.ID)
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		t.Fatal("token must contain iat and exp")
	}
	if claims.IssuedAt.Before(before) || claims.IssuedAt.After(after) {
		t.Errorf("iat=%v, want between %v and %v", claims.IssuedAt.Time, before, after)
	}
	if claims.ExpiresAt.Before(before.Add(cfg.JWTTtl)) || claims.ExpiresAt.After(after.Add(cfg.JWTTtl)) {
		t.Errorf("exp=%v, want current time plus %v", claims.ExpiresAt.Time, cfg.JWTTtl)
	}
	id, err := auth.ParseToken(signed)
	if err != nil || id != user.ID {
		t.Errorf("ParseToken(Login())=(%s, %v), want (%s, nil)", id, err, user.ID)
	}
}

func TestLoginInvalidInput(t *testing.T) {
	for _, input := range []service.LoginInput{
		{Email: "not-an-email", Password: "password"},
		{Email: "", Password: "password"},
		{Email: "user@example.com", Password: ""},
		{Email: "user@example.com", Password: strings.Repeat("a", 73)},
	} {
		t.Run(fmt.Sprintf("email=%q/password bytes=%d", input.Email, len(input.Password)), func(t *testing.T) {
			// An unexpected repository call panics in the stub.
			auth := service.NewAuth(testAuthConfig(), userRepositoryStub{}, testLogger())
			token, err := auth.Login(context.Background(), input)
			if token != "" || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Login() token empty=%v error=%v, want empty and ErrInvalidInput", token == "", err)
			}
		})
	}
}

func TestLoginErrors(t *testing.T) {
	hash := testPasswordHash(t, "password")
	storageErr := errors.New("storage unavailable")
	tests := []struct {
		name    string
		hash    string
		repoErr error
		want    error
	}{
		{"wrong password", testPasswordHash(t, "other password"), nil, domain.ErrInvalidCredentials},
		{"missing user", "", fmt.Errorf("get user: %w", domain.ErrUserNotFound), domain.ErrInvalidCredentials},
		{"database failure", "", storageErr, storageErr},
		{"corrupt hash", "broken-hash", nil, bcrypt.ErrHashTooShort},
		{"canceled context", hash, context.Canceled, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if errors.Is(tt.want, context.Canceled) {
				cancel()
			}
			calls := 0
			repo := userRepositoryStub{getByEmail: func(gotCtx context.Context, _ string) (*domain.User, error) {
				calls++
				if gotCtx != ctx {
					t.Error("GetByEmail received a different context")
				}
				if err := gotCtx.Err(); err != nil {
					return nil, err
				}
				if tt.repoErr != nil {
					return nil, tt.repoErr
				}
				return &domain.User{ID: uuid.New(), PasswordHash: tt.hash}, nil
			}}
			token, err := service.NewAuth(testAuthConfig(), repo, testLogger()).Login(ctx,
				service.LoginInput{Email: "user@example.com", Password: "password"})
			if token != "" || !errors.Is(err, tt.want) || calls != 1 {
				t.Errorf("Login() token empty=%v error=%v calls=%d, want empty, %v and 1", token == "", err, calls, tt.want)
			}
		})
	}
}

func TestParseToken(t *testing.T) {
	cfg := testAuthConfig()
	id := uuid.New()
	now := time.Now().Unix()
	tests := []struct {
		name    string
		method  jwt.SigningMethod
		key     string
		claims  jwt.MapClaims
		wantErr bool
	}{
		{"valid", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": id.String(), "exp": now + 3600}, false},
		{"wrong signature", jwt.SigningMethodHS256, "different-test-secret", jwt.MapClaims{"sub": id.String(), "exp": now + 3600}, true},
		{"wrong algorithm", jwt.SigningMethodHS384, cfg.JWTSecret, jwt.MapClaims{"sub": id.String(), "exp": now + 3600}, true},
		{"expired", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": id.String(), "exp": now - 3600}, true},
		{"missing exp", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": id.String()}, true},
		{"invalid exp type", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": id.String(), "exp": "tomorrow"}, true},
		{"missing sub", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"exp": now + 3600}, true},
		{"empty sub", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": "", "exp": now + 3600}, true},
		{"invalid UUID", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": "not-a-uuid", "exp": now + 3600}, true},
		{"invalid sub type", jwt.SigningMethodHS256, cfg.JWTSecret, jwt.MapClaims{"sub": 123, "exp": now + 3600}, true},
	}
	auth := service.NewAuth(cfg, userRepositoryStub{}, testLogger())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signed, err := jwt.NewWithClaims(tt.method, tt.claims).SignedString([]byte(tt.key))
			if err != nil {
				t.Fatalf("prepare token: %v", err)
			}
			got, err := auth.ParseToken(signed)
			if tt.wantErr {
				if err == nil || got != uuid.Nil {
					t.Errorf("ParseToken()=(%s, %v), want nil UUID and error", got, err)
				}
				return
			}
			if err != nil || got != id {
				t.Errorf("ParseToken()=(%s, %v), want (%s, nil)", got, err, id)
			}
		})
	}
	for _, malformed := range []string{"", "not-a-token", "e30.e30.%%%"} {
		t.Run("malformed/"+malformed, func(t *testing.T) {
			got, err := auth.ParseToken(malformed)
			if err == nil || got != uuid.Nil {
				t.Errorf("ParseToken()=(%s, %v), want nil UUID and error", got, err)
			}
		})
	}
}

func testAuthConfig() config.Auth {
	return config.Auth{JWTSecret: "test-only-secret-32-bytes-or-longer", JWTTtl: 5 * time.Minute}
}

func testPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("prepare password hash: %v", err)
	}
	return string(hash)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validRegisterInput() service.RegisterInput {
	return service.RegisterInput{Email: "user@example.com", Name: "User", Password: "password"}
}

func maxLengthEmail() string {
	return strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 62)
}
