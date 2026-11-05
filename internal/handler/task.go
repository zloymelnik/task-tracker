package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type TaskService interface {
	Create(ctx context.Context, actorID uuid.UUID, input service.CreateTaskInput) (*domain.Task, error)
	List(ctx context.Context, actorID uuid.UUID, filter domain.TaskFilter) ([]domain.Task, int, error)
	Update(ctx context.Context, actorID, taskID uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error)
	History(ctx context.Context, actorID uuid.UUID, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error)
	AddComment(ctx context.Context, actorID, taskID uuid.UUID, input service.CreateTaskCommentInput) (*domain.TaskComment, error)
	Comments(ctx context.Context, actorID uuid.UUID, filter domain.TaskCommentFilter) ([]domain.TaskComment, int, error)
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
			h.log.Error("Failed to write create task history response", "error", err)
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

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write create task response", "error", err)
		}
		return
	}
	query := r.URL.Query()
	filter := domain.TaskFilter{
		Limit:  20,
		Offset: 0,
	}

	teamID, err := uuid.Parse(query.Get("team_id"))
	if err != nil {
		respondError(w, domain.ErrInvalidInput, h.log)
		return
	}
	filter.TeamID = &teamID

	if query.Has("status") {
		status := domain.TaskStatus(query.Get("status"))
		filter.Status = &status
	}

	if query.Has("assignee_id") {
		assigneeID, err := uuid.Parse(query.Get("assignee_id"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.AssigneeID = &assigneeID
	}

	if query.Has("limit") {
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Limit = limit
	}

	if query.Has("offset") {
		offset, err := strconv.Atoi(query.Get("offset"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Offset = offset
	}
	h.log.Info("Gets task list", "filter", filter)
	tasks, total, err := h.s.List(r.Context(), userID, filter)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusOK, NewCreateTaskListResponse(tasks, total, filter.Limit, filter.Offset)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write task list response", "error", err)
		}
		return
	}
	h.log.Info("Gets task list successfully", "total", total, "filter", filter)
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	actorID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write update task response", "error", err)
		}
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		if err := writeErrorResponse(w, http.StatusBadRequest, "Failed task_id"); err != nil {
			h.log.Error("Failed to write update task response", "error", err)
		}
		return
	}

	var rawTask UpdateTaskRequest
	if err := decodeJSON(w, r, &rawTask); err != nil {
		respondError(w, err, h.log)
		return
	}
	taskInput, err := rawTask.ToInput()
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	h.log.Info("Update task", "taskID", taskID, "actorID", actorID, "body", taskInput)
	task, err := h.s.Update(r.Context(), actorID, taskID, taskInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}

	if err := writeJSON(w, http.StatusOK, NewCreateTaskResponse(*task)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write update task response", "error", err)
		}
		return
	}
	h.log.Info("Task update successfully", "id", task.ID, "task", task)
}

func (h *TaskHandler) History(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write history of task response", "error", err)
		}
		return
	}
	query := r.URL.Query()
	filter := domain.TaskHistoryFilter{
		Limit:  20,
		Offset: 0,
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, domain.ErrInvalidInput, h.log)
		return
	}
	filter.TaskID = &taskID
	if query.Has("limit") {
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Limit = limit
	}

	if query.Has("offset") {
		offset, err := strconv.Atoi(query.Get("offset"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Offset = offset
	}

	h.log.Info("Gets task history", "filter", filter)
	history, total, err := h.s.History(r.Context(), userID, filter)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusOK, NewCreateListTaskHistoryResponse(history, total, filter.Limit, filter.Offset)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write task history response", "error", err)
		}
		return
	}
	h.log.Info("Gets task history successfully", "total", total, "filter", filter)
}

func (h *TaskHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write create task comment response", "error", err)
		}
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, domain.ErrInvalidInput, h.log)
		return
	}
	var rawComment CreateTaskCommentRequest
	if err := decodeJSON(w, r, &rawComment); err != nil {
		respondError(w, err, h.log)
		return
	}
	commentInput := rawComment.ToInput()
	h.log.Info("Create task comment", "taskID", taskID, "userID", userID, "comment", commentInput)
	comment, err := h.s.AddComment(r.Context(), userID, taskID, commentInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusCreated, NewCreateTaskCommentResponse(*comment)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write create task comment response", "error", err)
		}
		return
	}
	h.log.Info("Task comment created successfully", "taskID", taskID, "userID", userID, "comment", commentInput)
}

func (h *TaskHandler) Comments(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write comments of task response", "error", err)
		}
		return
	}
	query := r.URL.Query()
	filter := domain.TaskCommentFilter{
		Limit:  20,
		Offset: 0,
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, domain.ErrInvalidInput, h.log)
		return
	}
	filter.TaskID = &taskID
	if query.Has("limit") {
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Limit = limit
	}

	if query.Has("offset") {
		offset, err := strconv.Atoi(query.Get("offset"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Offset = offset
	}

	h.log.Info("Gets task comments", "filter", filter)
	comments, total, err := h.s.Comments(r.Context(), userID, filter)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusOK, NewCreateListTaskCommentsResponse(comments, total, filter.Limit, filter.Offset)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write task comments response", "error", err)
		}
		return
	}
	h.log.Info("Gets task comments successfully", "total", total, "filter", filter)
}
