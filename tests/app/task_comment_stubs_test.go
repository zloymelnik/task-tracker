package app_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func (f createTaskRouteFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in create-task route test")
}
func (f createTaskRouteFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in create-task route test")
}
func (f listTaskRouteFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in list-task route test")
}
func (f listTaskRouteFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in list-task route test")
}
func (f updateTaskRouteFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in update-task route test")
}
func (f updateTaskRouteFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in update-task route test")
}
func (f taskHistoryRouteFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in history route test")
}
func (f taskHistoryRouteFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in history route test")
}
