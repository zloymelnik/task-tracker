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

// Create creates a new task.
// @Summary Создание задачи
// @Description Создавать задачу может любой участник команды; исполнитель должен состоять в той же команде; начальные значения — todo и version = 1
// @Tags tasks
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body CreateTaskRequest true "Данные задачи"
// @Success 201 {object} CreateTaskResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 413 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks [post]
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

// List gets list of tasks.
// @Summary Список задач команды
// @Description Требуется членство в команде. Фильтры применяются одновременно (AND).
// @Tags tasks
// @Security BearerAuth
// @Produce json
// @Param team_id query string true "UUID команды" Format(uuid)
// @Param status query string false "Статус задачи" Enums(todo,in_progress,done)
// @Param assignee_id query string false "UUID исполнителя" Format(uuid)
// @Param limit query int false "Размер страницы" default(20) minimum(1) maximum(100)
// @Param offset query int false "Смещение" default(0) minimum(0)
// @Success 200 {object} ListTasksResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks [get]
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

// Update changes task data.
// @Summary Обновление задачи
// @Description Требуется членство в команде. Owner и admin могут менять любые задачи команды.
// @Description Автор задачи может менять название, описание, статус и исполнителя; исполнитель без прав автора — только статус.
// @Description Поле version обязательно: при несовпадении с текущей версией возвращается 409.
// @Description Отсутствующие поля сохраняются. assignee_id: UUID — назначить участника команды, null — снять исполнителя.
// @Description Фактические изменения увеличивают version и сохраняются вместе с историей в одной транзакции.
// @Description Запрос без изменений возвращает текущую задачу без увеличения версии и новой записи истории.
// @Description Для title, description и status значение null трактуется как отсутствие поля. Для очистки описания передайте пустую строку.
// @Tags tasks
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body UpdateTaskRequest true "Доступные для изменения данные задачи"
// @Param id path string true "UUID задачи" Format(uuid)
// @Success 200 {object} CreateTaskResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 404 {object} errorResponse
// @Failure 409 {object} errorResponse
// @Failure 413 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks/{id} [put]
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

// History returns task change history.
// @Summary История изменений задачи
// @Description Доступна участникам команды задачи. Возвращает старые и новые значения, автора и время изменений; порядок — created_at DESC, id DESC.
// @Tags tasks
// @Security BearerAuth
// @Produce json
// @Param id path string true "UUID задачи" Format(uuid)
// @Param limit query int false "Размер страницы" default(20) minimum(1) maximum(100)
// @Param offset query int false "Смещение" default(0) minimum(0)
// @Success 200 {object} ListTaskHistoryResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 404 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks/{id}/history [get]
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

// AddComment creates a task comment.
// @Summary Добавление комментария к задаче
// @Description Доступно участникам команды задачи. Автор определяется по JWT. Текст не может состоять только из пробелов; максимум — 65535 байт при общем лимите JSON-запроса 64 КиБ.
// @Description Добавление комментария не изменяет версию задачи и не создаёт запись в истории изменений.
// @Tags tasks
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "UUID задачи" Format(uuid)
// @Param request body CreateTaskCommentRequest true "Текст комментария"
// @Success 201 {object} TaskCommentResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 404 {object} errorResponse
// @Failure 413 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks/{id}/comments [post]
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

// Comments returns task comments.
// @Summary Список комментариев задачи
// @Description Доступен участникам команды задачи. Возвращает страницу с общим количеством комментариев; порядок — created_at DESC, id DESC. Пустой список возвращается как items: [].
// @Tags tasks
// @Security BearerAuth
// @Produce json
// @Param id path string true "UUID задачи" Format(uuid)
// @Param limit query int false "Размер страницы" default(20) minimum(1) maximum(100)
// @Param offset query int false "Смещение" default(0) minimum(0)
// @Success 200 {object} ListTaskCommentsResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 404 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /tasks/{id}/comments [get]
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
