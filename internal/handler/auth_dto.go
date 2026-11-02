package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type RegisterRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

// ToInput converts the HTTP request into service input without changing its values.
func (r RegisterRequest) ToInput() service.RegisterInput {
	return service.RegisterInput{
		Email:    r.Email,
		Name:     r.Name,
		Password: r.Password,
	}
}

// NewUserResponse selects the user fields exposed by the API.
func NewUserResponse(user domain.User) UserResponse {
	return UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		CreatedAt: user.CreatedAt,
	}
}

// ToInput converts the HTTP request into service input without changing its values.
func (r LoginRequest) ToInput() service.LoginInput {
	return service.LoginInput{
		Email:    r.Email,
		Password: r.Password,
	}
}

// NewUserLogin selects the user fields exposed by the API.
func NewUserLogin(token string) LoginResponse {
	return LoginResponse{
		Token: token,
	}
}
