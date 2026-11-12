package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type cachedTaskPage struct {
	Items []domain.Task `json:"items"`
	Total int           `json:"total"`
}

func generateTaskListVersionKey(teamID uuid.UUID) string {
	return "cache:tasks:team:" + teamID.String() + ":version"
}

type TaskCacheOptions struct {
	TTL         time.Duration
	LoadTimeout time.Duration
}

func NewTaskCacheOptions(cfg config.Redis) TaskCacheOptions {
	return TaskCacheOptions{
		TTL:         cfg.CacheTTL,
		LoadTimeout: cfg.LoadTimeout,
	}
}

func generateTaskListCacheKey(filter domain.TaskFilter, generation string) string {
	teamIDStr := "all"
	teamID := filter.TeamID
	if teamID != nil {
		teamIDStr = teamID.String()
	}
	statusStr := "all"
	status := filter.Status
	if status != nil {
		statusStr = string(*status)
	}
	assigneeIDStr := "all"
	assigneeID := filter.AssigneeID
	if assigneeID != nil {
		assigneeIDStr = assigneeID.String()
	}
	return "cache:tasks:team:" + teamIDStr + ":generation:" + generation + ":status:" + statusStr + ":assignee:" + assigneeIDStr + ":limit:" + strconv.Itoa(filter.Limit) + ":offset:" + strconv.Itoa(filter.Offset)
}

func (s *TaskService) getTaskListGeneration(ctx context.Context, teamID uuid.UUID) (string, error) {
	key := generateTaskListVersionKey(teamID)
	version, hit, err := s.cache.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("get list generation: %w", err)
	}
	id, parseErr := uuid.Parse(string(version))
	valid := hit && parseErr == nil && id != uuid.Nil
	if !valid {
		if err := s.cache.Invalidate(ctx, key); err != nil {
			return "", fmt.Errorf("get list generation: %w", err)
		}
		version, hit, err = s.cache.Get(ctx, key)
		if err != nil {
			return "", fmt.Errorf("get list generation: %w", err)
		}
		id, parseErr = uuid.Parse(string(version))
		valid = hit && parseErr == nil && id != uuid.Nil
		if !valid {
			return "", fmt.Errorf("cache generation missing after initialization")
		}
	}
	return id.String(), nil
}

func (s *TaskService) taskPageFromCache(ctx context.Context, key string) (cachedTaskPage, bool, error) {
	data, hit, err := s.cache.Get(ctx, key)
	if err != nil {
		return cachedTaskPage{}, false, fmt.Errorf("failed get task list: %w", err)
	}
	if !hit {
		return cachedTaskPage{}, false, nil
	}
	var page *cachedTaskPage
	if err := json.Unmarshal(data, &page); err != nil {
		return cachedTaskPage{}, false, fmt.Errorf("decode cached task list: %w", err)
	}
	if page == nil || page.Total < 0 || len(page.Items) > page.Total {
		return cachedTaskPage{}, false, fmt.Errorf("incorrect cached task list")
	}
	if page.Items == nil {
		page.Items = []domain.Task{}
	}
	return *page, true, nil
}

func (s *TaskService) taskPageToCache(
	ctx context.Context,
	key string,
	page cachedTaskPage,
) error {
	if page.Items == nil {
		page.Items = []domain.Task{}
	}
	data, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("data serialization error: %w", err)
	}
	if err := s.cache.Set(ctx, key, data, s.cacheOptions.TTL); err != nil {
		return fmt.Errorf("error data saving: %w", err)
	}
	return nil
}

func (s *TaskService) loadTaskPage(
	ctx context.Context,
	filter domain.TaskFilter,
	key string,
) (cachedTaskPage, error) {
	resultCh := s.taskLoads.DoChan(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cacheOptions.LoadTimeout)
		defer cancel()
		cache, hit, err := s.taskPageFromCache(loadCtx, key)
		if err == nil && hit {
			return cache, nil
		}
		if err != nil {
			s.log.Warn("failed to read task page from cache", "error", err)
		}
		tasks, total, err := s.tr.List(loadCtx, filter)
		if err != nil {
			return nil, fmt.Errorf("get task list: %w", err)
		}
		newCache := cachedTaskPage{
			Items: tasks,
			Total: total,
		}
		if err := s.taskPageToCache(loadCtx, key, newCache); err != nil {
			s.log.Error("wrong set data to cache", "error", err)
			return newCache, nil
		}
		return newCache, nil
	})
	select {
	case <-ctx.Done():
		return cachedTaskPage{}, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return cachedTaskPage{}, result.Err
		}
		return result.Val.(cachedTaskPage), nil
	}
}
