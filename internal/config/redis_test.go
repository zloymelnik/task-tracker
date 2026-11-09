package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zloymelnik/task-tracker/internal/config"
)

func TestLoadConfigRedisEnvOverridesYAML(t *testing.T) {
	isolateConfigEnv(t)
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	for key, value := range map[string]string{
		"REDIS_ADDR": "cache.example:6380", "REDIS_DB": "2",
		"REDIS_PASSWORD": "test-only-redis-password", "REDIS_POOL_SIZE": "25",
		"REDIS_LOAD_TIMEOUT": "3s", "REDIS_MAX_REQUEST_TIME": "2s", "REDIS_CACHE_TTL": "10m",
		"REDIS_RETRY_MAX_ATTEMPTS": "3", "REDIS_RETRY_DELAY": "500ms",
	} {
		t.Setenv(key, value)
	}
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Redis{Addr: "cache.example:6380", DB: 2, Password: "test-only-redis-password", PoolSize: 25, MaxRequestTime: 2 * time.Second, CacheTTL: 10 * time.Minute, LoadTimeout: 3 * time.Second, Retry: config.RedisRetry{MaxAttempts: 3, Delay: 500 * time.Millisecond}}
	if got.Redis != want {
		t.Errorf("Redis=%+v, want %+v", got.Redis, want)
	}
}

func TestLoadConfigInvalidRedisEnv(t *testing.T) {
	for _, tt := range []struct{ key, value, message string }{
		{"REDIS_ADDR", "", "wrong addr"},
		{"REDIS_ADDR", " \t ", "wrong addr"},
		{"REDIS_DB", "-1", "wrong db"},
		{"REDIS_DB", "abc", "REDIS_DB"},
		{"REDIS_DB", "99999999999999999999999", "REDIS_DB"},
		{"REDIS_POOL_SIZE", "0", "wrong pool_size"},
		{"REDIS_POOL_SIZE", "-1", "wrong pool_size"},
		{"REDIS_POOL_SIZE", "abc", "REDIS_POOL_SIZE"},
		{"REDIS_POOL_SIZE", "99999999999999999999999", "REDIS_POOL_SIZE"},
		{"REDIS_MAX_REQUEST_TIME", "0s", "wrong max_request_time"},
		{"REDIS_MAX_REQUEST_TIME", "-1s", "wrong max_request_time"},
		{"REDIS_MAX_REQUEST_TIME", "invalid", "REDIS_MAX_REQUEST_TIME"},
		{"REDIS_MAX_REQUEST_TIME", "99999999999999999999999h", "REDIS_MAX_REQUEST_TIME"},
		{"REDIS_CACHE_TTL", "0s", "wrong cache_ttl"},
		{"REDIS_CACHE_TTL", "-1s", "wrong cache_ttl"},
		{"REDIS_CACHE_TTL", "invalid", "REDIS_CACHE_TTL"},
		{"REDIS_CACHE_TTL", "99999999999999999999999h", "REDIS_CACHE_TTL"},
		{"REDIS_LOAD_TIMEOUT", "0s", "wrong load_timeout"},
		{"REDIS_LOAD_TIMEOUT", "-1s", "wrong load_timeout"},
		{"REDIS_LOAD_TIMEOUT", "invalid", "REDIS_LOAD_TIMEOUT"},
		{"REDIS_LOAD_TIMEOUT", "99999999999999999999999h", "REDIS_LOAD_TIMEOUT"},
		{"REDIS_RETRY_MAX_ATTEMPTS", "0", "wrong Redis max_attempts"},
		{"REDIS_RETRY_MAX_ATTEMPTS", "-1", "wrong Redis max_attempts"},
		{"REDIS_RETRY_MAX_ATTEMPTS", "invalid", "REDIS_RETRY_MAX_ATTEMPTS"},
		{"REDIS_RETRY_MAX_ATTEMPTS", "99999999999999999999999", "REDIS_RETRY_MAX_ATTEMPTS"},
		{"REDIS_RETRY_DELAY", "0s", "wrong Redis delay"},
		{"REDIS_RETRY_DELAY", "-1s", "wrong Redis delay"},
		{"REDIS_RETRY_DELAY", "invalid", "REDIS_RETRY_DELAY"},
		{"REDIS_RETRY_DELAY", "99999999999999999999999h", "REDIS_RETRY_DELAY"},
	} {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			t.Setenv(tt.key, tt.value)
			_, err := config.LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("LoadConfig error=%v, want %q", err, tt.message)
			}
		})
	}
}

func TestLoadConfigInvalidRedisYAML(t *testing.T) {
	for _, tt := range []struct{ name, old, replacement, message string }{
		{"blank address", "  addr: redis:6379", `  addr: "   "`, "wrong addr"},
		{"negative DB", "  db: 0", "  db: -1", "wrong db"},
		{"zero pool", "  pool_size: 10", "  pool_size: 0", "wrong pool_size"},
		{"zero timeout", "  max_request_time: 5s", "  max_request_time: 0s", "wrong max_request_time"},
		{"zero TTL", "  cache_ttl: 5m", "  cache_ttl: 0s", "wrong cache_ttl"},
		{"zero load timeout", "  load_timeout: 5s", "  load_timeout: 0s", "wrong load_timeout"},
		{"negative load timeout", "  load_timeout: 5s", "  load_timeout: -1s", "wrong load_timeout"},
		{"missing load timeout", "  load_timeout: 5s\n", "", "wrong load_timeout"},
		{"zero attempts", "    max_attempts: 10", "    max_attempts: 0", "wrong Redis max_attempts"},
		{"negative attempts", "    max_attempts: 10", "    max_attempts: -1", "wrong Redis max_attempts"},
		{"zero retry delay", "    delay: 2s", "    delay: 0s", "wrong Redis delay"},
		{"negative retry delay", "    delay: 2s", "    delay: -1s", "wrong Redis delay"},
		{"missing retry", "  cache_ttl: 5m\n  load_timeout: 5s\n  retry:\n    max_attempts: 10\n    delay: 2s", "  cache_ttl: 5m\n  load_timeout: 5s", "wrong Redis max_attempts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Redis is before MySQL in the fixture, so replace its first match.
			updated := strings.Replace(string(contents), tt.old, tt.replacement, 1)
			if updated == string(contents) {
				t.Fatal("test fixture field not found")
			}
			if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = config.LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), "wrong redis settings") || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("LoadConfig error=%v, want Redis error %q", err, tt.message)
			}
		})
	}
}

func TestLoadConfigRedisPasswordOnlyFromEnv(t *testing.T) {
	for _, password := range []string{"", "test-only-env-redis-password"} {
		name := "without password"
		if password != "" {
			name = "env password overrides YAML"
		}
		t.Run(name, func(t *testing.T) {
			isolateConfigEnv(t)
			if password != "" {
				t.Setenv("REDIS_PASSWORD", password)
			}
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			updated := strings.Replace(string(contents), "redis:\n", "redis:\n  password: test-only-yaml-password\n", 1)
			if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := config.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.Redis.Password != password {
				t.Error("Redis password must be read only from environment")
			}
		})
	}
}

func TestLoadConfigRedisBoundaries(t *testing.T) {
	isolateConfigEnv(t)
	t.Setenv("REDIS_DB", "0")
	t.Setenv("REDIS_POOL_SIZE", "1")
	t.Setenv("REDIS_MAX_REQUEST_TIME", "1ns")
	t.Setenv("REDIS_CACHE_TTL", "1ns")
	t.Setenv("REDIS_LOAD_TIMEOUT", "1ns")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_RETRY_MAX_ATTEMPTS", "1")
	t.Setenv("REDIS_RETRY_DELAY", "1ns")
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	got, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Redis.DB != 0 || got.Redis.PoolSize != 1 || got.Redis.MaxRequestTime != time.Nanosecond || got.Redis.CacheTTL != time.Nanosecond || got.Redis.LoadTimeout != time.Nanosecond || got.Redis.Password != "" || got.Redis.Retry.MaxAttempts != 1 || got.Redis.Retry.Delay != time.Nanosecond {
		t.Errorf("incorrect Redis boundaries: %+v", got.Redis)
	}
}

func TestLoadConfigMissingRedisSection(t *testing.T) {
	isolateConfigEnv(t)
	path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	start, end := strings.Index(text, "redis:\n"), strings.Index(text, "mysql:\n")
	if start < 0 || end <= start {
		t.Fatal("Redis fixture section not found")
	}
	if err := os.WriteFile(path, []byte(text[:start]+text[end:]), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = config.LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "wrong redis settings") {
		t.Fatalf("missing Redis section error=%v", err)
	}
}
