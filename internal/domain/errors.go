package domain

import "errors"

var (
	ErrUserNotFound            = errors.New("user not found")
	ErrEmailAlreadyExists      = errors.New("email already exists")
	ErrInvalidInput            = errors.New("invalid input")
	ErrInvalidCredentials      = errors.New("invalid credentials")
	ErrTeamMemberNotFound      = errors.New("team member not found")
	ErrForbidden               = errors.New("forbidden")
	ErrTeamMemberAlreadyExists = errors.New("team member already exists")
	ErrTaskNotFound            = errors.New("task not found")
	ErrTaskVersionConflict     = errors.New("task version conflict")
)
