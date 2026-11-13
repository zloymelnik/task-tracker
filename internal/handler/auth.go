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

// Register creates a user.
// @Summary Регистрация пользователя
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Данные регистрации"
// @Success 201 {object} UserResponse
// @Failure 400 {object} errorResponse
// @Failure 409 {object} errorResponse
// @Failure 413 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Failure 429 {object} errorResponse "Превышен лимит запросов"
// @Header 429 {integer} Retry-After "Время ожидания перед повторной попыткой в секундах, минимум 1"
// @Router /register [post]
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

// Login creates a user's access token.
// @Summary Вход пользователя
// @Description Возвращает JWT в поле token. Для защищённых запросов передавайте Authorization: Bearer <JWT>.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Данные входа"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 413 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Failure 429 {object} errorResponse "Превышен лимит запросов"
// @Header 429 {integer} Retry-After "Время ожидания перед повторной попыткой в секундах, минимум 1"
// @Router /login [post]
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
