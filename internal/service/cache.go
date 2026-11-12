package service

import (
	"context"
	"time"
)

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, data []byte, ttl time.Duration) error
	Invalidate(ctx context.Context, versionKey string) error
}
