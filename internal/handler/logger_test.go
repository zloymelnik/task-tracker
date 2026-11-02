package handler_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zloymelnik/task-tracker/internal/handler"
)

func TestLoggerResponseStatus(t *testing.T) {
	for _, tt := range []struct {
		name        string
		writeStatus int
		wantStatus  int
	}{
		{"empty response", 0, http.StatusOK},
		{"explicit status", http.StatusUnauthorized, http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			calls := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tt.writeStatus != 0 {
					w.WriteHeader(tt.writeStatus)
				}
			})
			rec := httptest.NewRecorder()
			handler.Logger(log)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
			if calls != 1 || rec.Result().StatusCode != tt.wantStatus {
				t.Fatalf("handler calls=%d response status=%d, want 1/%d", calls, rec.Result().StatusCode, tt.wantStatus)
			}
			var entry struct {
				Status int `json:"status"`
			}
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("decode request log: %v", err)
			}
			if entry.Status != tt.wantStatus {
				t.Errorf("logged status=%d, want %d", entry.Status, tt.wantStatus)
			}
		})
	}
}
