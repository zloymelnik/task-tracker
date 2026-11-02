package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/zloymelnik/task-tracker/internal/domain"
)

var errEncodeJSON = errors.New("encode JSON response")

type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes before sending headers. Encoding errors wrap errEncodeJSON;
// after a write error the response has started and must not be replaced.
func writeJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%w: %w", errEncodeJSON, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

func writeErrorResponse(w http.ResponseWriter, status int, message string) error {
	return writeJSON(w, status, errorResponse{Error: message})
}

// respondError maps shared request and domain errors to public API responses.
// Internal diagnostics are logged once here and are never sent to the client.
func respondError(w http.ResponseWriter, err error, log *slog.Logger) {
	status, message := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, errBodyTooLarge):
		status, message = http.StatusRequestEntityTooLarge, "request body too large"
	case errors.Is(err, errInvalidJSON):
		status, message = http.StatusBadRequest, "invalid JSON"
	case errors.Is(err, domain.ErrInvalidInput):
		status, message = http.StatusBadRequest, "invalid input"
	case errors.Is(err, domain.ErrEmailAlreadyExists):
		status, message = http.StatusConflict, "email already exists"
	case errors.Is(err, domain.ErrInvalidCredentials):
		status, message = http.StatusUnauthorized, "invalid credentials"
	default:
		log.Error("Request failed", "error", err)
	}
	if err := writeErrorResponse(w, status, message); err != nil {
		log.Error("Failed to write error response", "error", err)
	}
}
