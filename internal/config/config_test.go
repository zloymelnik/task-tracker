package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
				Env: wantEnv,
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
		Env: "prod",
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
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func isolateConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ENV", "APP_PORT", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_SHUTDOWN_TIMEOUT"} {
		// Setenv registers cleanup to restore the original environment.
		// Unsetenv lets cleanenv use YAML values and defaults during this test.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}
