package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type AuthService interface {
	Register(ctx context.Context, input service.RegisterInput) (*domain.User, error)
	Login(ctx context.Context, input service.LoginInput) (string, error)
}

type AuthHandler struct {
	s   AuthService
	log *slog.Logger
}

func NewAuthHandler(s AuthService, log *slog.Logger) *AuthHandler {
	return &AuthHandler{
		s:   s,
		log: log,
	}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var rawUser RegisterRequest
	if err := decodeJSON(w, r, &rawUser); err != nil {
		respondError(w, err, h.log)
		return
	}

	userInput := rawUser.ToInput()
	h.log.Info("Register user", "email", userInput.Email, "name", userInput.Name)
	user, err := h.s.Register(r.Context(), userInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}

	if err := writeJSON(w, http.StatusCreated, NewUserResponse(*user)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write registration response", "error", err)
		}
		return
	}
	h.log.Info("User registered successfully", "email", user.Email, "name", user.Name, "id", user.ID)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var rawUser LoginRequest
	if err := decodeJSON(w, r, &rawUser); err != nil {
		respondError(w, err, h.log)
		return
	}
	userInput := rawUser.ToInput()
	h.log.Info("Login user", "email", userInput.Email)
	token, err := h.s.Login(r.Context(), userInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}

	if err := writeJSON(w, http.StatusOK, NewUserLogin(token)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write login response", "error", err)
		}
		return
	}
	h.log.Info("User login successfully")
}
