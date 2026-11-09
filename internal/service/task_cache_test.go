package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type taskCacheStub struct {
	get        func(context.Context, string) ([]byte, bool, error)
	set        func(context.Context, string, []byte, time.Duration) error
	invalidate func(context.Context, string) error
}

func (c taskCacheStub) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return c.get(ctx, key)
}
func (c taskCacheStub) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return c.set(ctx, key, data, ttl)
}
func (c taskCacheStub) Invalidate(ctx context.Context, key string) error {
	return c.invalidate(ctx, key)
}

func taskCacheOptions() service.TaskCacheOptions {
	return service.TaskCacheOptions{TTL: 5 * time.Minute, LoadTimeout: time.Second}
}
func cacheTestMembership(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
	return &domain.TeamMember{Role: domain.MemberRole}, nil
}
func cacheVersionKey(team uuid.UUID) string { return "cache:tasks:team:" + team.String() + ":version" }
func cachePageKey(f domain.TaskFilter, generation string) string {
	status, assignee := "all", "all"
	if f.Status != nil {
		status = string(*f.Status)
	}
	if f.AssigneeID != nil {
		assignee = f.AssigneeID.String()
	}
	return fmt.Sprintf("cache:tasks:team:%s:generation:%s:status:%s:assignee:%s:limit:%d:offset:%d", f.TeamID.String(), generation, status, assignee, f.Limit, f.Offset)
}

func TestTaskListCachePaths(t *testing.T) {
	for _, name := range []string{"hit", "miss", "broken JSON", "null page", "negative total", "too many items", "read failure", "write failure", "generation failure", "database failure", "empty hit"} {
		t.Run(name, func(t *testing.T) {
			team, actor, generation := uuid.New(), uuid.New(), uuid.NewString()
			status, assignee := domain.InProgress, uuid.New()
			filter := domain.TaskFilter{TeamID: &team, Status: &status, AssigneeID: &assignee, Limit: 3, Offset: 6}
			want := []domain.Task{{ID: uuid.New(), TeamID: team, Title: "Задача", Status: status, AssigneeID: &assignee, Version: 2}}
			total := 9
			if name == "empty hit" {
				want = []domain.Task{}
				total = 0
			}
			sentinel := errors.New("storage unavailable")
			var reads, writes, dbCalls int
			cache := taskCacheStub{
				get: func(_ context.Context, key string) ([]byte, bool, error) {
					reads++
					if key == cacheVersionKey(team) {
						if name == "generation failure" {
							return nil, false, sentinel
						}
						return []byte(generation), true, nil
					}
					if key != cachePageKey(filter, generation) {
						t.Errorf("wrong cache key: %s", key)
					}
					switch name {
					case "hit", "empty hit":
						data, e := json.Marshal(struct {
							Items []domain.Task `json:"items"`
							Total int           `json:"total"`
						}{want, total})
						return data, true, e
					case "broken JSON":
						return []byte("{"), true, nil
					case "null page":
						return []byte("null"), true, nil
					case "negative total":
						return []byte(`{"items":[],"total":-1}`), true, nil
					case "too many items":
						return []byte(`{"items":[{}],"total":0}`), true, nil
					case "read failure":
						return nil, false, sentinel
					default:
						return nil, false, nil
					}
				},
				set: func(_ context.Context, key string, data []byte, ttl time.Duration) error {
					writes++
					if key != cachePageKey(filter, generation) || ttl != 5*time.Minute {
						t.Errorf("wrong key/TTL: %s %s", key, ttl)
					}
					var page struct {
						Items []domain.Task `json:"items"`
						Total int           `json:"total"`
					}
					if err := json.Unmarshal(data, &page); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(page.Items, want) || page.Total != total {
						t.Errorf("cached page=%+v", page)
					}
					if name == "write failure" {
						return sentinel
					}
					return nil
				},
				invalidate: func(context.Context, string) error { t.Error("unexpected generation initialization"); return nil },
			}
			repo := listTaskRepositoryFunc(func(_ context.Context, got domain.TaskFilter) ([]domain.Task, int, error) {
				dbCalls++
				if !reflect.DeepEqual(got, filter) {
					t.Errorf("filter=%+v", got)
				}
				if name == "database failure" {
					return nil, 0, sentinel
				}
				return want, total, nil
			})
			svc := service.NewTaskService(nil, repo, nil, nil, listTaskMembershipFunc(cacheTestMembership), cache, taskCacheOptions(), testLogger())
			got, count, err := svc.List(context.Background(), actor, filter)
			if name == "database failure" {
				if !errors.Is(err, sentinel) || got != nil || count != 0 || dbCalls != 1 || writes != 0 {
					t.Fatalf("result=%v,%d,%v; db=%d writes=%d", got, count, err, dbCalls, writes)
				}
				return
			}
			if err != nil || count != total || !reflect.DeepEqual(got, want) {
				t.Fatalf("List=%v,%d,%v", got, count, err)
			}
			expectedDB, expectedWrites := 1, 1
			if name == "hit" || name == "empty hit" {
				expectedDB, expectedWrites = 0, 0
			}
			if name == "generation failure" {
				expectedWrites = 0
			}
			if dbCalls != expectedDB || writes != expectedWrites || reads < 1 {
				t.Errorf("db=%d writes=%d reads=%d", dbCalls, writes, reads)
			}
		})
	}
}

func TestTaskCacheGenerationInitialization(t *testing.T) {
	for _, name := range []string{"missing", "malformed", "zero UUID", "invalidation failure", "reread failure", "still missing"} {
		t.Run(name, func(t *testing.T) {
			team, actor, generation := uuid.New(), uuid.New(), uuid.NewString()
			filter := domain.TaskFilter{TeamID: &team, Limit: 20}
			reads, invalidations, dbCalls := 0, 0, 0
			sentinel := errors.New("redis failed")
			cache := taskCacheStub{
				get: func(_ context.Context, key string) ([]byte, bool, error) {
					if key != cacheVersionKey(team) {
						return nil, false, nil
					}
					reads++
					if reads == 1 {
						switch name {
						case "malformed":
							return []byte("invalid"), true, nil
						case "zero UUID":
							return []byte(uuid.Nil.String()), true, nil
						}
						return nil, false, nil
					}
					if name == "reread failure" {
						return nil, false, sentinel
					}
					if name == "still missing" {
						return nil, false, nil
					}
					return []byte(generation), true, nil
				},
				invalidate: func(_ context.Context, key string) error {
					invalidations++
					if key != cacheVersionKey(team) {
						t.Error("wrong version key")
					}
					if name == "invalidation failure" {
						return sentinel
					}
					return nil
				},
				set: func(_ context.Context, key string, data []byte, _ time.Duration) error {
					if key != cachePageKey(filter, generation) || !strings.Contains(string(data), `"items":[]`) {
						t.Errorf("wrong page: %s %s", key, data)
					}
					return nil
				},
			}
			repo := listTaskRepositoryFunc(func(context.Context, domain.TaskFilter) ([]domain.Task, int, error) {
				dbCalls++
				return []domain.Task{}, 0, nil
			})
			svc := service.NewTaskService(nil, repo, nil, nil, listTaskMembershipFunc(cacheTestMembership), cache, taskCacheOptions(), testLogger())
			_, _, err := svc.List(context.Background(), actor, filter)
			if err != nil || invalidations != 1 || dbCalls != 1 {
				t.Errorf("err=%v invalidations=%d DB=%d", err, invalidations, dbCalls)
			}
		})
	}
}

func TestTaskCacheDeniedBeforeRead(t *testing.T) {
	for _, role := range []domain.TeamRole{"", "unknown"} {
		t.Run(string(role), func(t *testing.T) {
			team := uuid.New()
			cache := taskCacheStub{get: func(context.Context, string) ([]byte, bool, error) {
				t.Error("cache accessed before permission check")
				return nil, false, nil
			}}
			members := listTaskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				if role == "" {
					return nil, domain.ErrTeamMemberNotFound
				}
				return &domain.TeamMember{Role: role}, nil
			})
			svc := service.NewTaskService(nil, nil, nil, nil, members, cache, taskCacheOptions(), testLogger())
			_, _, err := svc.List(context.Background(), uuid.New(), domain.TaskFilter{TeamID: &team, Limit: 20})
			if !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestTaskCacheSharedLoadCancellation(t *testing.T) {
	team, actor, generation := uuid.New(), uuid.New(), uuid.NewString()
	filter := domain.TaskFilter{TeamID: &team, Limit: 20}
	var pageMu sync.Mutex
	var storedPage []byte
	started, release, finished := make(chan context.Context, 1), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var dbCalls atomic.Int32
	repo := listTaskRepositoryFunc(func(ctx context.Context, _ domain.TaskFilter) ([]domain.Task, int, error) {
		dbCalls.Add(1)
		started <- ctx
		select {
		case <-release:
			return []domain.Task{{ID: team, TeamID: team}}, 1, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	})
	cache := taskCacheStub{get: func(_ context.Context, key string) ([]byte, bool, error) {
		if key == cacheVersionKey(team) {
			return []byte(generation), true, nil
		}
		pageMu.Lock()
		defer pageMu.Unlock()
		return storedPage, storedPage != nil, nil
	}, set: func(_ context.Context, _ string, data []byte, _ time.Duration) error {
		pageMu.Lock()
		defer pageMu.Unlock()
		storedPage = data
		return nil
	}}
	svc := service.NewTaskService(nil, repo, nil, nil, listTaskMembershipFunc(cacheTestMembership), cache, taskCacheOptions(), testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(finished)
		_, _, err := svc.List(ctx, actor, filter)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("leader error=%v", err)
		}
	}()
	var loadCtx context.Context
	select {
	case loadCtx = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not return")
	}
	if loadCtx.Err() != nil {
		t.Fatal("leader cancellation canceled shared load")
	}
	// Each call registers in DoChan before its canceled caller stops waiting.
	for i := 0; i < 10; i++ {
		waitCtx, stop := context.WithCancel(context.Background())
		stop()
		_, _, err := svc.List(waitCtx, actor, filter)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("waiter error=%v", err)
		}
	}
	releaseOnce.Do(func() { close(release) })
	// Join the still-running shared load; it must complete for an active caller.
	got, total, err := svc.List(context.Background(), actor, filter)
	if err != nil || total != 1 || len(got) != 1 {
		t.Fatalf("survivor=%v,%d,%v", got, total, err)
	}
	if dbCalls.Load() != 1 {
		t.Fatalf("SQL calls=%d, want 1", dbCalls.Load())
	}
}

func TestTaskCacheLoadTimeout(t *testing.T) {
	team, generation := uuid.New(), uuid.NewString()
	var calls atomic.Int32
	cache := taskCacheStub{get: func(_ context.Context, key string) ([]byte, bool, error) {
		if key == cacheVersionKey(team) {
			return []byte(generation), true, nil
		}
		return nil, false, nil
	}}
	repo := listTaskRepositoryFunc(func(ctx context.Context, _ domain.TaskFilter) ([]domain.Task, int, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, 0, ctx.Err()
	})
	opts := taskCacheOptions()
	opts.LoadTimeout = 20 * time.Millisecond
	svc := service.NewTaskService(nil, repo, nil, nil, listTaskMembershipFunc(cacheTestMembership), cache, opts, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := svc.List(ctx, uuid.New(), domain.TaskFilter{TeamID: &team, Limit: 20})
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("error=%v calls=%d parent=%v", err, calls.Load(), ctx.Err())
	}
}

func TestTaskCacheMutationInvalidation(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, outcome := range []string{"success", "no changes", "write failure", "history failure", "commit failure", "invalidation failure"} {
			if operation == "create" && outcome == "no changes" {
				continue
			}
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				task := updateTestTask()
				sentinel := errors.New("operation failed")
				committed := false
				invalidations := 0
				tx := transactionManagerFunc(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}
					if outcome == "commit failure" {
						return sentinel
					}
					committed = true
					return nil
				})
				members := taskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
					return &domain.TeamMember{Role: domain.MemberRole}, nil
				})
				history := taskHistoryRepositoryFunc(func(context.Context, *domain.TaskHistory) error {
					if outcome == "history failure" {
						return sentinel
					}
					return nil
				})
				cache := taskCacheStub{invalidate: func(_ context.Context, key string) error {
					invalidations++
					if !committed {
						t.Error("cache invalidated before commit")
					}
					if key != cacheVersionKey(task.TeamID) {
						t.Errorf("wrong version key: %s", key)
					}
					if outcome == "invalidation failure" {
						return sentinel
					}
					return nil
				}}
				var got *domain.Task
				var err error
				if operation == "create" {
					repo := taskRepositoryFunc(func(context.Context, *domain.Task) error {
						if outcome == "write failure" {
							return sentinel
						}
						return nil
					})
					svc := service.NewTaskService(tx, repo, history, nil, members, cache, taskCacheOptions(), testLogger())
					got, err = svc.Create(context.Background(), task.CreatedBy, service.CreateTaskInput{TeamID: task.TeamID, Title: "New"})
				} else {
					repo := updateTaskRepositoryStub{get: func(context.Context, uuid.UUID) (*domain.Task, error) { copy := task; return &copy, nil }, update: func(_ context.Context, updated *domain.Task, _ uint32) error {
						if outcome == "write failure" {
							return sentinel
						}
						updated.Version++
						return nil
					}}
					input := service.UpdateTaskInput{Version: task.Version}
					title := "Changed"
					if outcome != "no changes" {
						input.Title = &title
					}
					svc := service.NewTaskService(tx, repo, history, nil, members, cache, taskCacheOptions(), testLogger())
					got, err = svc.Update(context.Background(), task.CreatedBy, task.ID, input)
				}
				fail := outcome == "write failure" || outcome == "history failure" || outcome == "commit failure"
				if fail {
					if !errors.Is(err, sentinel) || got != nil {
						t.Fatalf("result=%v,%v", got, err)
					}
				} else if err != nil || got == nil {
					t.Fatalf("result=%v,%v", got, err)
				}
				want := 0
				if !fail && outcome != "no changes" {
					want = 1
				}
				if invalidations != want {
					t.Errorf("invalidations=%d want %d", invalidations, want)
				}
			})
		}
	}
}

func TestTaskCacheInvalidationDuringLoad(t *testing.T) {
	team, actor := uuid.New(), uuid.New()
	oldGeneration, newGeneration := uuid.NewString(), uuid.NewString()
	filter := domain.TaskFilter{TeamID: &team, Limit: 20}
	var mu sync.Mutex
	data := map[string][]byte{cacheVersionKey(team): []byte(oldGeneration)}
	cache := taskCacheStub{
		get: func(_ context.Context, key string) ([]byte, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			value, hit := data[key]
			return value, hit, nil
		},
		set: func(_ context.Context, key string, value []byte, _ time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			data[key] = value
			return nil
		},
		invalidate: func(_ context.Context, key string) error {
			mu.Lock()
			defer mu.Unlock()
			data[key] = []byte(newGeneration)
			return nil
		},
	}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var calls atomic.Int32
	oldTask, newTask := domain.Task{ID: uuid.New(), Title: "Old"}, domain.Task{ID: uuid.New(), Title: "New"}
	repo := listTaskRepositoryFunc(func(ctx context.Context, _ domain.TaskFilter) ([]domain.Task, int, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return []domain.Task{oldTask}, 1, nil
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
		return []domain.Task{newTask}, 1, nil
	})
	svc := service.NewTaskService(nil, repo, nil, nil, listTaskMembershipFunc(cacheTestMembership), cache, taskCacheOptions(), testLogger())
	go func() {
		defer close(done)
		_, _, err := svc.List(context.Background(), actor, filter)
		if err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	if err := cache.Invalidate(context.Background(), cacheVersionKey(team)); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not finish")
	}
	got, _, err := svc.List(context.Background(), actor, filter)
	if err != nil || len(got) != 1 || got[0].Title != "New" || calls.Load() != 2 {
		t.Fatalf("new generation returned %v error=%v calls=%d", got, err, calls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := data[cachePageKey(filter, oldGeneration)]; !ok {
		t.Error("in-flight load did not retain its original generation")
	}
	if _, ok := data[cachePageKey(filter, newGeneration)]; !ok {
		t.Error("new generation page missing")
	}
}
