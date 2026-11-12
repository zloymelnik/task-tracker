package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

type Cache struct {
	client  *goredis.Client
	timeout time.Duration
	log     *slog.Logger
}

func NewCache(client *goredis.Client, timeout time.Duration, log *slog.Logger) *Cache {
	return &Cache{
		client:  client,
		timeout: timeout,
		log:     log,
	}
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	c.log.Debug("Redis: Get cache", "key", key)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("redis: failed get cache %q: %w", key, err)
	}
	c.log.Debug("Redis: Get cache hit", "key", key)
	return data, true, nil
}

func (c *Cache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	c.log.Debug("Redis: Set cache", "key", key)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("redis: failed set cache %q: %w", key, err)
	}

	c.log.Debug("Redis: Set cache succesfully", "key", key)
	return nil
}

func (c *Cache) Invalidate(ctx context.Context, versionKey string) error {
	generation := uuid.NewString()
	c.log.Debug("Redis: Set cache versionKey", "key", versionKey, "value", generation)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.client.Set(ctx, versionKey, generation, 0).Err(); err != nil {
		return fmt.Errorf("redis: failed to invalidate cache %q: %w", versionKey, err)
	}
	c.log.Debug("Redis: Set cache versionKey succesfully", "key", versionKey, "value", generation)
	return nil
}
