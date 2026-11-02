package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zloymelnik/task-tracker/internal/config"
)

func TestLoadConfigYAML(t *testing.T) {
	isolateConfigEnv(t)

	for _, env := range []string{"local", "dev", "prod", ""} {
		t.Run("env="+env, func(t *testing.T) {
			path := writeConfig(t, env, 8080, "5s", "10s", "15s")
			got, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() error: %v", err)
			}

			wantEnv := env
			if wantEnv == "" {
				wantEnv = "local"
			}
			want := config.Config{
				Env:  wantEnv,
				DB:   validTestDB(),
				Auth: validTestAuth(),
				Server: config.Server{
					Port:            8080,
					ReadTimeout:     5 * time.Second,
					WriteTimeout:    10 * time.Second,
					ShutdownTimeout: 15 * time.Second,
				},
			}
			if *got != want {
				t.Errorf("LoadConfig() = %+v, want %+v", *got, want)
			}
		})
	}
}

func TestLoadConfigEnvOverridesYAML(t *testing.T) {
	isolateConfigEnv(t)
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	t.Setenv("ENV", "prod")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("SERVER_READ_TIMEOUT", "2s")
	t.Setenv("SERVER_WRITE_TIMEOUT", "3s")
	t.Setenv("SERVER_SHUTDOWN_TIMEOUT", "4s")

	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	want := config.Config{
		Env:  "prod",
		DB:   validTestDB(),
		Auth: validTestAuth(),
		Server: config.Server{
			Port:            9090,
			ReadTimeout:     2 * time.Second,
			WriteTimeout:    3 * time.Second,
			ShutdownTimeout: 4 * time.Second,
		},
	}
	if *got != want {
		t.Errorf("LoadConfig() = %+v, want %+v", *got, want)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := config.LoadConfig(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadConfig() error = %v, want os.ErrNotExist", err)
	}
}

func TestLoadConfigEmptyPath(t *testing.T) {
	if _, err := config.LoadConfig(""); err == nil {
		t.Fatal("LoadConfig() succeeded with an empty path")
	}
}

func TestLoadConfigInvalidValues(t *testing.T) {
	isolateConfigEnv(t)
	tests := []struct {
		name     string
		env      string
		port     int
		read     string
		write    string
		shutdown string
	}{
		{"unknown environment", "staging", 8080, "5s", "10s", "15s"},
		{"negative port", "local", -1, "5s", "10s", "15s"},
		{"zero port", "local", 0, "5s", "10s", "15s"},
		{"port above maximum", "local", 65536, "5s", "10s", "15s"},
		{"negative read timeout", "local", 8080, "-1s", "10s", "15s"},
		{"zero read timeout", "local", 8080, "0s", "10s", "15s"},
		{"negative write timeout", "local", 8080, "5s", "-1s", "15s"},
		{"zero write timeout", "local", 8080, "5s", "0s", "15s"},
		{"negative shutdown timeout", "local", 8080, "5s", "10s", "-1s"},
		{"zero shutdown timeout", "local", 8080, "5s", "10s", "0s"},
		{"invalid duration", "local", 8080, "invalid", "10s", "15s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.env, tt.port, tt.read, tt.write, tt.shutdown)
			if _, err := config.LoadConfig(path); err == nil {
				t.Fatal("LoadConfig() succeeded with invalid values")
			}
		})
	}
}

func writeConfig(t *testing.T, env string, port int, read, write, shutdown string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("env: %q\nserver:\n  port: %d\n  read_timeout: %q\n  write_timeout: %q\n  shutdown_timeout: %q\n", env, port, read, write, shutdown)
	content += `auth:
  jwt_ttl: 5m
mysql:
  host: mysql
  port: 3306
  name: team_tasks
  user: app
  max_request_time: 5s
  pool:
    max_open_conns: 10
    max_idle_conns: 5
    conn_max_lifetime: 30m
  retry:
    max_attempts: 10
    delay: 2s
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func isolateConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ENV", "APP_PORT", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_SHUTDOWN_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD",
		"DB_POOL_MAX_REQUEST_TIME", "DB_POOL_MAX_OPEN_CONNS", "DB_POOL_MAX_IDLE_CONNS",
		"DB_POOL_CONN_MAX_LIFETIME", "DB_RETRY_MAX_ATTEMPTS", "DB_RETRY_DELAY",
		"JWT_SECRET", "JWT_TTL",
	} {
		// Setenv registers cleanup to restore the original environment.
		// Unsetenv lets cleanenv use YAML values and defaults during this test.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Setenv("DB_PASSWORD", "test-password")
	t.Setenv("JWT_SECRET", "test-only-jwt-secret")
}

func validTestAuth() config.Auth {
	return config.Auth{JWTSecret: "test-only-jwt-secret", JWTTtl: 5 * time.Minute}
}

func TestLoadConfigJWTEnvOverridesYAML(t *testing.T) {
	isolateConfigEnv(t)
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	t.Setenv("JWT_SECRET", "other-test-jwt-secret")
	t.Setenv("JWT_TTL", "15m")
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	want := config.Auth{JWTSecret: "other-test-jwt-secret", JWTTtl: 15 * time.Minute}
	if got.Auth != want {
		t.Errorf("Auth = %+v, want %+v", got.Auth, want)
	}
}

func TestLoadConfigInvalidJWTValues(t *testing.T) {
	for _, tt := range []struct{ key, value, wantError string }{
		{"JWT_SECRET", "", "wrong jwt secret"},
		{"JWT_TTL", "0s", "wrong jwt ttl"},
		{"JWT_TTL", "-1s", "wrong jwt ttl"},
		{"JWT_TTL", "invalid", "JWT_TTL"},
	} {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			t.Setenv(tt.key, tt.value)
			_, err := config.LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("LoadConfig() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadConfigIgnoresYAMLJWTSecret(t *testing.T) {
	isolateConfigEnv(t)
	if err := os.Unsetenv("JWT_SECRET"); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "auth:\n", "auth:\n  jwt_secret: yaml-secret\n", 1))
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "wrong jwt secret") {
		t.Fatalf("LoadConfig() error = %v, want missing JWT secret", err)
	}
	if got.Auth.JWTSecret != "" {
		t.Fatal("JWT secret must not be loaded from YAML")
	}
}

func validTestDB() config.DB {
	return config.DB{
		Host: "mysql", Port: 3306, Name: "team_tasks", User: "app",
		Password: "test-password", MaxRequestTime: 5 * time.Second,
		Pool:  config.DBPool{MaxOpenConns: 10, MaxIdleConns: 5, ConnMaxLifetime: 30 * time.Minute},
		Retry: config.DBRetry{MaxAttempts: 10, Delay: 2 * time.Second},
	}
}

func TestLoadConfigDBEnvOverridesYAML(t *testing.T) {
	isolateConfigEnv(t)
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	for key, value := range map[string]string{
		"DB_HOST": "db.example", "DB_PORT": "3307", "DB_NAME": "other_tasks",
		"DB_USER": "other_user", "DB_PASSWORD": "other-password",
		"DB_POOL_MAX_REQUEST_TIME": "7s", "DB_POOL_MAX_OPEN_CONNS": "20",
		"DB_POOL_MAX_IDLE_CONNS": "8", "DB_POOL_CONN_MAX_LIFETIME": "15m",
		"DB_RETRY_MAX_ATTEMPTS": "3", "DB_RETRY_DELAY": "1s",
	} {
		t.Setenv(key, value)
	}
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	want := config.DB{
		Host: "db.example", Port: 3307, Name: "other_tasks", User: "other_user",
		Password: "other-password", MaxRequestTime: 7 * time.Second,
		Pool:  config.DBPool{MaxOpenConns: 20, MaxIdleConns: 8, ConnMaxLifetime: 15 * time.Minute},
		Retry: config.DBRetry{MaxAttempts: 3, Delay: time.Second},
	}
	if got.DB != want {
		t.Errorf("DB = %+v, want %+v", got.DB, want)
	}
}

func TestLoadConfigInvalidDBValues(t *testing.T) {
	tests := []struct{ key, value, wantError string }{
		{"DB_HOST", "", "wrong DB host"},
		{"DB_PORT", "-1", "port must be between"},
		{"DB_PORT", "0", "port must be between"},
		{"DB_PORT", "65536", "port must be between"},
		{"DB_PORT", "invalid", "DB_PORT"},
		{"DB_NAME", "", "wrong DB name"},
		{"DB_USER", "", "wrong DB user"},
		{"DB_PASSWORD", "", "wrong DB password"},
		{"DB_POOL_MAX_OPEN_CONNS", "0", "wrong DB max_open_conns"},
		{"DB_POOL_MAX_OPEN_CONNS", "-1", "wrong DB max_open_conns"},
		{"DB_POOL_MAX_IDLE_CONNS", "0", "wrong DB max_idle_conns"},
		{"DB_POOL_MAX_IDLE_CONNS", "-1", "wrong DB max_idle_conns"},
		{"DB_POOL_CONN_MAX_LIFETIME", "0s", "wrong DB conn_max_lifetime"},
		{"DB_POOL_CONN_MAX_LIFETIME", "-1s", "wrong DB conn_max_lifetime"},
		{"DB_POOL_MAX_REQUEST_TIME", "0s", "wrong DB max_request_time"},
		{"DB_POOL_MAX_REQUEST_TIME", "-1s", "wrong DB max_request_time"},
		{"DB_POOL_MAX_REQUEST_TIME", "invalid", "DB_POOL_MAX_REQUEST_TIME"},
		{"DB_RETRY_MAX_ATTEMPTS", "0", "wrong DB max_attempts"},
		{"DB_RETRY_MAX_ATTEMPTS", "-1", "wrong DB max_attempts"},
		{"DB_RETRY_DELAY", "0s", "wrong DB delay"},
		{"DB_RETRY_DELAY", "-1s", "wrong DB delay"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			t.Setenv(tt.key, tt.value)
			_, err := config.LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("LoadConfig() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadConfigDBPortBoundaries(t *testing.T) {
	for _, port := range []string{"1", "65535"} {
		t.Run(port, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv("DB_PORT", port)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			if _, err := config.LoadConfig(path); err != nil {
				t.Fatalf("LoadConfig() error: %v", err)
			}
		})
	}
}

func TestLoadConfigIgnoresYAMLPassword(t *testing.T) {
	isolateConfigEnv(t)
	if err := os.Unsetenv("DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Append to the mysql block; the retry mapping ends when indentation decreases.
	content = append(content, []byte("  password: yaml-password\n")...)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "wrong DB password") {
		t.Fatalf("LoadConfig() error = %v, want missing password", err)
	}
	if got.DB.Password != "" {
		t.Fatal("password must not be loaded from YAML")
	}
}

func TestProjectConfig(t *testing.T) {
	isolateConfigEnv(t)
	got, err := config.LoadConfig("../../configs/config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig(project YAML) error: %v", err)
	}
	if got.DB != validTestDB() {
		t.Errorf("project DB = %+v, want %+v", got.DB, validTestDB())
	}
}
