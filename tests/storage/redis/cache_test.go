package redis_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	redisstorage "github.com/zloymelnik/task-tracker/internal/storage/redis"
)

func cacheLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newCacheFixture(t *testing.T) (*goredis.Client, *redisstorage.Cache, func(string) string) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR to run Redis integration tests")
	}
	db := 0
	if value := os.Getenv("REDIS_TEST_DB"); value != "" {
		var err error
		db, err = strconv.Atoi(value)
		if err != nil || db < 0 {
			t.Fatal("invalid REDIS_TEST_DB")
		}
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr, Password: os.Getenv("REDIS_TEST_PASSWORD"), DB: db,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		PoolTimeout: time.Second, ContextTimeoutEnabled: true, MaxRetries: -1})
	prefix := "test:task-tracker:cache:" + uuid.NewString() + ":"
	var keys []string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				t.Errorf("clean up Redis test keys: %v", err)
			}
		}
		if err := client.Close(); err != nil {
			t.Errorf("close Redis test client: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis test connection failed: %v", err)
	}
	key := func(suffix string) string { k := prefix + suffix; keys = append(keys, k); return k }
	return client, redisstorage.NewCache(client, time.Second, cacheLogger()), key
}

func TestCacheMissAndRoundTrip(t *testing.T) {
	_, cache, key := newCacheFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, found, err := cache.Get(ctx, key("missing"))
	if data != nil || found || err != nil {
		t.Fatalf("missing Get()=(%v,%v,%v), want nil/false/nil", data, found, err)
	}
	for _, tt := range []struct {
		name  string
		value []byte
	}{
		{"JSON", []byte(`{"items":[],"total":42}`)},
		{"Unicode", []byte(" \nЗадачи команды\n ")},
		{"binary", []byte{0, 255, 1, 128, 13, 10}},
		{"empty", []byte{}},
		{"nil", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k := key(tt.name)
			if err := cache.Set(ctx, k, tt.value, time.Minute); err != nil {
				t.Fatal(err)
			}
			got, found, err := cache.Get(ctx, k)
			if err != nil || !found || !bytes.Equal(got, tt.value) {
				t.Errorf("Get()=(%v,%v,%v), want %v/true/nil", got, found, err, tt.value)
			}
			// SET overwrites the same key, rather than appending to its value.
			if err := cache.Set(ctx, k, []byte("replacement"), time.Minute); err != nil {
				t.Fatal(err)
			}
			got, found, err = cache.Get(ctx, k)
			if err != nil || !found || string(got) != "replacement" {
				t.Errorf("overwritten Get()=(%q,%v,%v)", got, found, err)
			}
		})
	}
}

func TestCacheTTL(t *testing.T) {
	client, cache, key := newCacheFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k := key("expires")
	if err := cache.Set(ctx, k, []byte("temporary"), time.Second); err != nil {
		t.Fatal(err)
	}
	ttl, err := client.PTTL(ctx, k).Result()
	if err != nil || ttl <= 0 || ttl > time.Second {
		t.Fatalf("PTTL()=(%v,%v), want positive TTL <= 1s", ttl, err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, found, err := cache.Get(ctx, k)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			if data != nil {
				t.Error("expired miss must return nil data")
			}
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("cache key did not expire")
		}
	}
	// A zero TTL creates a persistent entry and clears an existing expiration.
	k = key("persistent")
	if err := cache.Set(ctx, k, []byte("old"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, k, []byte("persistent"), 0); err != nil {
		t.Fatal(err)
	}
	ttl, err = client.PTTL(ctx, k).Result()
	if err != nil || ttl != -1 {
		t.Errorf("persistent PTTL()=(%v,%v), want -1/nil", ttl, err)
	}
	data, found, err := cache.Get(ctx, k)
	if err != nil || !found || string(data) != "persistent" {
		t.Errorf("persistent Get()=(%q,%v,%v)", data, found, err)
	}
}

func TestCacheInvalidate(t *testing.T) {
	client, cache, key := newCacheFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	versionKey, otherKey, pageKey := key("tasks:version"), key("comments:version"), key("old-page")
	if err := cache.Set(ctx, otherKey, []byte("other version"), 0); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, pageKey, []byte("old page"), time.Minute); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for range 3 {
		if err := cache.Invalidate(ctx, versionKey); err != nil {
			t.Fatal(err)
		}
		data, found, err := cache.Get(ctx, versionKey)
		if err != nil || !found {
			t.Fatalf("version Get()=(%q,%v,%v)", data, found, err)
		}
		id, err := uuid.Parse(string(data))
		if err != nil || id == uuid.Nil || id.Version() != 4 || string(data) == previous {
			t.Errorf("invalid or unchanged generation %q: %v", data, err)
		}
		previous = string(data)
		ttl, err := client.PTTL(ctx, versionKey).Result()
		if err != nil || ttl != -1 {
			t.Errorf("generation PTTL()=(%v,%v), want persistent", ttl, err)
		}
		// Ensure replacing an expiring version also removes the old TTL.
		if err := client.Expire(ctx, versionKey, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for k, want := range map[string]string{otherKey: "other version", pageKey: "old page"} {
		data, found, err := cache.Get(ctx, k)
		if err != nil || !found || string(data) != want {
			t.Errorf("Invalidate changed unrelated key: data=%q found=%v err=%v", data, found, err)
		}
	}
}

func TestCacheWrongType(t *testing.T) {
	client, cache, key := newCacheFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k := key("list")
	if err := client.RPush(ctx, k, "item").Err(); err != nil {
		t.Fatal(err)
	}
	data, found, err := cache.Get(ctx, k)
	var redisErr goredis.Error
	if data != nil || found || !errors.As(err, &redisErr) || !strings.HasPrefix(redisErr.Error(), "WRONGTYPE") {
		t.Errorf("wrong-type Get()=(%v,%v,%v), want wrapped WRONGTYPE", data, found, err)
	}
}

type cacheProcessHook struct {
	run func(context.Context, goredis.Cmder) error
}

func (h cacheProcessHook) DialHook(next goredis.DialHook) goredis.DialHook     { return next }
func (h cacheProcessHook) ProcessHook(goredis.ProcessHook) goredis.ProcessHook { return h.run }
func (h cacheProcessHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func cacheOperations(cache *redisstorage.Cache) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"Get": func(ctx context.Context) error {
			data, found, err := cache.Get(ctx, "test-key")
			if err != nil && (data != nil || found) {
				return errors.New("Get returned data on failure")
			}
			return err
		},
		"Set":        func(ctx context.Context) error { return cache.Set(ctx, "test-key", []byte("data"), time.Minute) },
		"Invalidate": func(ctx context.Context) error { return cache.Invalidate(ctx, "test-key") },
	}
}

func TestCacheOperationTimeouts(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	const timeout = 20 * time.Millisecond
	client.AddHook(cacheProcessHook{run: func(ctx context.Context, _ goredis.Cmder) error {
		// This hook blocks at the client boundary without making a connection.
		<-ctx.Done()
		return ctx.Err()
	}})
	cache := redisstorage.NewCache(client, timeout, cacheLogger())
	for name, run := range cacheOperations(cache) {
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := run(parent)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error=%v, want deadline exceeded", err)
			}
			if parent.Err() != nil {
				t.Error("operation failed to enforce its own timeout before parent deadline")
			}
		})
	}
}

func TestCacheContextAndWrappedErrors(t *testing.T) {
	for _, scenario := range []string{"canceled", "expired parent", "client failure", "closed client"} {
		t.Run(scenario, func(t *testing.T) {
			client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
			if scenario == "closed client" {
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Cleanup(func() {
					if err := client.Close(); err != nil {
						t.Error(err)
					}
				})
			}
			failure := errors.New("test Redis failure")
			if scenario != "closed client" {
				client.AddHook(cacheProcessHook{run: func(ctx context.Context, _ goredis.Cmder) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					return failure
				}})
			}
			cache := redisstorage.NewCache(client, time.Second, cacheLogger())
			ctx := context.Background()
			want := failure
			switch scenario {
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "expired parent":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				want = context.DeadlineExceeded
			case "closed client":
				want = goredis.ErrClosed
			}
			for name, run := range cacheOperations(cache) {
				t.Run(name, func(t *testing.T) {
					err := run(ctx)
					if !errors.Is(err, want) || !strings.Contains(err.Error(), "test-key") {
						t.Errorf("error=%v, want wrapped %v and key", err, want)
					}
				})
			}
		})
	}
}
