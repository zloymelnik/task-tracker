package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

// Exercise error classification through the public handler. Invalid decoder
// destinations inside decodeJSON are no longer tested directly.
func TestRegisterInternalJSONErrors(t *testing.T) {
	for _, tt := range []struct {
		name        string
		destination any
	}{
		{"value instead of pointer", handler.RegisterRequest{}},
		{"nil", nil},
		{"nil pointer", (*handler.RegisterRequest)(nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			internalErr := &json.InvalidUnmarshalError{Type: reflect.TypeOf(tt.destination)}
			calls := 0
			auth := authServiceFunc(func(context.Context, service.RegisterInput) (*domain.User, error) {
				calls++
				return nil, fmt.Errorf("private JSON operation: %w", internalErr)
			})
			h := handler.NewAuthHandler(auth, silentLogger())
			for _, body := range []string{`{"email":`, registerJSON} {
				rec := httptest.NewRecorder()
				h.Register(rec, httptest.NewRequest(http.MethodPost, "/api/v1/register", strings.NewReader(body)))
				status := http.StatusBadRequest
				if body == registerJSON {
					status = http.StatusInternalServerError
				}
				assertErrorResponse(t, rec, status)
				var response struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				want := "invalid JSON"
				if status == http.StatusInternalServerError {
					want = "internal server error"
				}
				if response.Error != want {
					t.Errorf("error=%q, want %q", response.Error, want)
				}
			}
			if calls != 1 {
				t.Errorf("service calls=%d, want only the valid request to reach service", calls)
			}
		})
	}
}
