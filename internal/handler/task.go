package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type TaskService interface {
	Create(ctx context.Context, actorID uuid.UUID, input service.CreateTaskInput) (*domain.Task, error)
}

type TaskHandler struct {
	s   TaskService
	log *slog.Logger
}

func NewTaskHandler(s TaskService, log *slog.Logger) *TaskHandler {
	return &TaskHandler{
		s:   s,
		log: log,
	}
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write create task response", "error", err)
		}
		return
	}
	var rawTask CreateTaskRequest
	if err := decodeJSON(w, r, &rawTask); err != nil {
		respondError(w, err, h.log)
		return
	}
	taskInput := rawTask.ToInput()
	h.log.Info("Create task", "name", taskInput.Title)
	task, err := h.s.Create(r.Context(), userID, taskInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusCreated, NewCreateTaskResponse(*task)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write create task response", "error", err)
		}
		return
	}
	h.log.Info("Task created successfully", "id", task.ID, "title", task.Title)
}
