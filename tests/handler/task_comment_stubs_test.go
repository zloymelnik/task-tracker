package handler_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func (f taskServiceFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in create-task test")
}
func (f taskServiceFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in create-task test")
}
func (f listTaskServiceFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in list-task test")
}
func (f listTaskServiceFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in list-task test")
}
func (f updateTaskServiceFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in update-task test")
}
func (f updateTaskServiceFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in update-task test")
}
func (f taskHistoryServiceFunc) AddComment(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	panic("unexpected AddComment in history test")
}
func (f taskHistoryServiceFunc) Comments(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	panic("unexpected Comments in history test")
}
