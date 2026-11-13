package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zloymelnik/task-tracker/internal/config"
)

func TestRateLimitEnvOverridesAreIndependent(t *testing.T) {
	for _, prefix := range []string{"RATE_LIMIT_", "AUTH_LOGIN_", "AUTH_REGISTER_"} {
		t.Run(prefix, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			t.Setenv(prefix+"INTERVAL", "250ms")
			t.Setenv(prefix+"BURST", "17")
			t.Setenv(prefix+"MAX_ENTRIES", "1234")
			got, err := config.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]config.RateLimit{
				"RATE_LIMIT_":    {Interval: 5 * time.Second, Burst: 10, MaxEntries: 1000},
				"AUTH_LOGIN_":    validTestAuth().LoginLimit,
				"AUTH_REGISTER_": validTestAuth().RegisterLimit,
			}
			want[prefix] = config.RateLimit{Interval: 250 * time.Millisecond, Burst: 17, MaxEntries: 1234}
			for key, limit := range map[string]config.RateLimit{
				"RATE_LIMIT_":    got.RateLimit,
				"AUTH_LOGIN_":    got.Auth.LoginLimit,
				"AUTH_REGISTER_": got.Auth.RegisterLimit,
			} {
				if limit != want[key] {
					t.Errorf("%s settings = %+v, want %+v", key, limit, want[key])
				}
			}
		})
	}
}

func TestInvalidRateLimitEnv(t *testing.T) {
	for _, target := range []struct{ prefix, context string }{
		{"RATE_LIMIT_", "wrong RateLimit settings"},
		{"AUTH_LOGIN_", "wrong login limit"},
		{"AUTH_REGISTER_", "wrong register limit"},
	} {
		for _, tt := range []struct{ field, value, message string }{
			{"INTERVAL", "0s", "wrong RateLimit Interval"},
			{"INTERVAL", "-1s", "wrong RateLimit Interval"},
			{"INTERVAL", "invalid", "INTERVAL"},
			{"INTERVAL", "999999999999999999999h", "INTERVAL"},
			{"MAX_ENTRIES", "0", "wrong RateLimit MaxEntries"},
			{"MAX_ENTRIES", "-1", "wrong RateLimit MaxEntries"},
			{"MAX_ENTRIES", "invalid", "MAX_ENTRIES"},
			{"MAX_ENTRIES", "999999999999999999999", "MAX_ENTRIES"},
			{"BURST", "0", "wrong RateLimit Burst"},
			{"BURST", "-1", "wrong RateLimit Burst"},
			{"BURST", "invalid", "BURST"},
			{"BURST", "999999999999999999999", "BURST"},
		} {
			key := target.prefix + tt.field
			t.Run(key+"="+tt.value, func(t *testing.T) {
				isolateConfigEnv(t)
				path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
				t.Setenv(key, tt.value)
				_, err := config.LoadConfig(path)
				if err == nil || !strings.Contains(err.Error(), tt.message) {
					t.Fatalf("error = %v, want containing %q", err, tt.message)
				}
				if strings.HasPrefix(tt.message, "wrong") {
					if !strings.Contains(err.Error(), target.context) {
						t.Errorf("error = %v, want context %q", err, target.context)
					}
				} else if !strings.Contains(err.Error(), key) {
					t.Errorf("parse error = %v, want variable name %q", err, key)
				}
			})
		}
	}
}

func TestRateLimitYAMLRequiredFields(t *testing.T) {
	for _, section := range []struct{ yaml, context string }{
		{"rate_limit:\n  interval: 5s\n  burst: 10\n  max_entries: 1000\n", "wrong RateLimit settings"},
		{"  login_limit:\n    interval: 6s\n    burst: 5\n    max_entries: 1000\n", "wrong login limit"},
		{"  register_limit:\n    interval: 60s\n    burst: 3\n    max_entries: 1000\n", "wrong register limit"},
	} {
		for _, field := range []string{"section", "interval", "burst", "max_entries"} {
			t.Run(section.context+"/missing_"+field, func(t *testing.T) {
				isolateConfigEnv(t)
				path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				replacement := ""
				if field != "section" {
					for _, line := range strings.SplitAfter(section.yaml, "\n") {
						if !strings.HasPrefix(strings.TrimSpace(line), field+":") {
							replacement += line
						}
					}
				}
				updated := strings.Replace(string(body), section.yaml, replacement, 1)
				if updated == string(body) {
					t.Fatal("rate limit fixture section not found")
				}
				if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
					t.Fatal(err)
				}
				_, err = config.LoadConfig(path)
				message := "wrong RateLimit Interval"
				switch field {
				case "burst":
					message = "wrong RateLimit Burst"
				case "max_entries":
					message = "wrong RateLimit MaxEntries"
				}
				if err == nil || !strings.Contains(err.Error(), section.context) || !strings.Contains(err.Error(), message) {
					t.Fatalf("error = %v, want %q and %q", err, section.context, message)
				}
			})
		}
	}
}
