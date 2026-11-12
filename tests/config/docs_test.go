package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/zloymelnik/task-tracker/internal/config"
)

func TestDocsConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, yaml, env string
		want            bool
		invalid         bool
	}{
		{name: "missing section defaults to disabled"},
		{name: "YAML enabled", yaml: "docs:\n  enabled: true\n", want: true},
		{name: "env enables docs", env: "true", want: true},
		{name: "env disables YAML", yaml: "docs:\n  enabled: true\n", env: "false"},
		{name: "invalid env", env: "invalid", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfigEnv(t)
			path := writeConfig(t, "local", 8080, "5s", "10s", "15s")
			if tt.yaml != "" {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := f.WriteString(tt.yaml)
				closeErr := f.Close()
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if tt.env != "" {
				t.Setenv("DOCS_ENABLED", tt.env)
			}
			got, err := config.LoadConfig(path)
			if tt.invalid {
				if err == nil || !strings.Contains(err.Error(), "DOCS_ENABLED") {
					t.Fatalf("error=%v want DOCS_ENABLED parse error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Docs.Enabled != tt.want {
				t.Errorf("Docs.Enabled=%v want %v", got.Docs.Enabled, tt.want)
			}
		})
	}
}
