package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONInvalidDestination(t *testing.T) {
	for _, tt := range []struct {
		name string
		dst  any
	}{
		{"value instead of pointer", RegisterRequest{}},
		{"nil", nil},
		{"nil pointer", (*RegisterRequest)(nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"email":"user@example.com"}`))
			rec := httptest.NewRecorder()
			err := decodeJSON(rec, req, tt.dst)
			var destinationErr *json.InvalidUnmarshalError
			if !errors.As(err, &destinationErr) {
				t.Fatalf("decodeJSON() error = %v, want wrapped InvalidUnmarshalError", err)
			}
			if errors.Is(err, errInvalidJSON) {
				t.Fatal("invalid destination must not be classified as client JSON error")
			}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			respondError(rec, err, log)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
		})
	}
}
