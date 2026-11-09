package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	redisstorage "github.com/zloymelnik/task-tracker/internal/storage/redis"
)

func taskTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Prefix every key and track exact names so cleanup never touches application data.
type isolatedTaskCache struct {
	service.Cache
	prefix string
	mu     sync.Mutex
	keys   map[string]struct{}
}

func (c *isolatedTaskCache) key(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	key = c.prefix + key
	c.keys[key] = struct{}{}
	return key
}
func (c *isolatedTaskCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return c.Cache.Get(ctx, c.key(key))
}
func (c *isolatedTaskCache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return c.Cache.Set(ctx, c.key(key), data, ttl)
}
func (c *isolatedTaskCache) Invalidate(ctx context.Context, key string) error {
	return c.Cache.Invalidate(ctx, c.key(key))
}

type countingCachedTaskRepository struct {
	service.TaskRepository
	calls atomic.Int32
}

func (r *countingCachedTaskRepository) List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, int, error) {
	r.calls.Add(1)
	return r.TaskRepository.List(ctx, f)
}

func TestTaskCacheWithMySQLAndRedis(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR and MYSQL_TEST_DSN to run cache integration test")
	}
	redisDB := 0
	if value := os.Getenv("REDIS_TEST_DB"); value != "" {
		var err error
		redisDB, err = strconv.Atoi(value)
		if err != nil || redisDB < 0 {
			t.Fatal("invalid REDIS_TEST_DB")
		}
	}
	_, repos, team, actor, _ := newTaskFixture(t)
	client := goredis.NewClient(&goredis.Options{Addr: addr, Password: os.Getenv("REDIS_TEST_PASSWORD"), DB: redisDB, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, ContextTimeoutEnabled: true, MaxRetries: -1})
	cache := &isolatedTaskCache{Cache: redisstorage.NewCache(client, time.Second, taskTestLogger()), prefix: "test:task-tracker:service:" + uuid.NewString() + ":", keys: make(map[string]struct{})}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cache.mu.Lock()
		keys := make([]string, 0, len(cache.keys))
		for key := range cache.keys {
			keys = append(keys, key)
		}
		cache.mu.Unlock()
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				t.Error(err)
			}
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	repo := &countingCachedTaskRepository{TaskRepository: repos.Task}
	svc := service.NewTaskService(repos.Transactions, repo, repos.TaskHistory, repos.TaskComment, repos.TeamMember, cache, service.TaskCacheOptions{TTL: time.Minute, LoadTimeout: 2 * time.Second}, taskTestLogger())
	task, err := svc.Create(ctx, actor.ID, service.CreateTaskInput{TeamID: team.ID, Title: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.TaskFilter{TeamID: &team.ID, Limit: 20}
	for i := 0; i < 2; i++ {
		items, total, err := svc.List(ctx, actor.ID, filter)
		if err != nil || total != 1 || len(items) != 1 || items[0].Title != "Before" {
			t.Fatalf("List=%v,%d,%v", items, total, err)
		}
	}
	if repo.calls.Load() != 1 {
		t.Fatalf("SQL calls=%d; repeated page should hit Redis", repo.calls.Load())
	}
	versionKey := "cache:tasks:team:" + team.ID.String() + ":version"
	before, _, err := cache.Get(ctx, versionKey)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Update(ctx, actor.ID, task.ID, service.UpdateTaskInput{Version: task.Version})
	if err != nil {
		t.Fatal(err)
	}
	afterNoop, _, err := cache.Get(ctx, versionKey)
	if err != nil || string(before) != string(afterNoop) {
		t.Fatal("no-op changed cache generation")
	}
	title := "After"
	_, err = svc.Update(ctx, actor.ID, task.ID, service.UpdateTaskInput{Version: task.Version, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := cache.Get(ctx, versionKey)
	if err != nil || string(before) == string(after) {
		t.Fatal("committed update did not change cache generation")
	}
	items, total, err := svc.List(ctx, actor.ID, filter)
	if err != nil || total != 1 || len(items) != 1 || items[0].Title != "After" || repo.calls.Load() != 2 {
		t.Fatalf("updated List=%v,%d,%v SQL=%d", items, total, err, repo.calls.Load())
	}
	_, _, err = svc.List(ctx, uuid.New(), filter)
	if !errors.Is(err, domain.ErrForbidden) || repo.calls.Load() != 2 {
		t.Fatalf("outsider error=%v SQL=%d", err, repo.calls.Load())
	}
	_, err = svc.Create(ctx, actor.ID, service.CreateTaskInput{TeamID: team.ID, Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	items, total, err = svc.List(ctx, actor.ID, filter)
	if err != nil || total != 2 || len(items) != 2 || repo.calls.Load() != 3 {
		t.Fatalf("created List=%v,%d,%v SQL=%d", items, total, err, repo.calls.Load())
	}
	cache.mu.Lock()
	keys := make([]string, 0, len(cache.keys))
	for key := range cache.keys {
		keys = append(keys, key)
	}
	cache.mu.Unlock()
	pages := 0
	for _, key := range keys {
		if !strings.Contains(key, ":generation:") {
			continue
		}
		pages++
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > time.Minute {
			t.Errorf("page TTL=%s error=%v", ttl, err)
		}
	}
	if pages != 3 {
		t.Errorf("cached generations=%d, want 3", pages)
	}
}
