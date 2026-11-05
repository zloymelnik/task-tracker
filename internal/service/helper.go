package service

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/zloymelnik/task-tracker/internal/domain"
)

func nameNormalize(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", domain.ErrInvalidInput)
	}
	if utf8.RuneCountInString(name) > 255 {
		return "", fmt.Errorf("%w: name must contain at most 255 characters", domain.ErrInvalidInput)
	}
	return name, nil
}

func emailNormalize(email string) (string, error) {
	email = strings.TrimSpace(email)

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", fmt.Errorf("%w: invalid email format", domain.ErrInvalidInput)
	}

	normalized := strings.ToLower(parsed.Address)
	if len(normalized) > 255 {
		return "", fmt.Errorf("%w: email must contain at most 255 characters", domain.ErrInvalidInput)
	}
	return normalized, nil
}
