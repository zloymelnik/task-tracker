package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxJSONBodyBytes = 64 << 10

var (
	errInvalidJSON  = errors.New("invalid JSON")
	errBodyTooLarge = errors.New("request body too large")
)

// decodeJSON reads a single JSON object into dst, which must be a non-nil pointer to a DTO.
// It does not write a response or validate business rules.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	var body json.RawMessage
	if err := decoder.Decode(&body); err != nil {
		return jsonRequestError(err)
	}
	if len(body) == 0 || body[0] != '{' {
		return errInvalidJSON
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return jsonRequestError(err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errInvalidJSON
		}
		return jsonRequestError(err)
	}
	return nil
}

func jsonRequestError(err error) error {
	var destinationErr *json.InvalidUnmarshalError
	if errors.As(err, &destinationErr) {
		return fmt.Errorf("decode JSON destination: %w", err)
	}
	var sizeErr *http.MaxBytesError
	if errors.As(err, &sizeErr) {
		return fmt.Errorf("%w: %w", errBodyTooLarge, err)
	}
	return fmt.Errorf("%w: %w", errInvalidJSON, err)
}
