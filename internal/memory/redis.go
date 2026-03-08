// Package memory — Redis warm backend implementation.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RedisClient abstracts Redis operations (can use go-redis, rueidis, etc.)
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
}

// RedisWarm implements WarmBackend using Redis.
type RedisWarm struct {
	client    RedisClient
	keyPrefix string
}

// NewRedisWarm creates a Redis-backed warm store.
func NewRedisWarm(client RedisClient) *RedisWarm {
	return &RedisWarm{client: client, keyPrefix: "waoo:mem:"}
}

func (r *RedisWarm) Get(ctx context.Context, key string) (*Entry, error) {
	data, err := r.client.Get(ctx, r.keyPrefix+key)
	if err != nil {
		return nil, nil // Treat as cache miss
	}
	if data == "" {
		return nil, nil
	}

	var entry Entry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &entry, nil
}

func (r *RedisWarm) Set(ctx context.Context, key string, entry *Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	ttl := entry.TTL
	if ttl == 0 {
		ttl = 30 * time.Minute // Default warm TTL
	}
	return r.client.Set(ctx, r.keyPrefix+key, string(data), ttl)
}

func (r *RedisWarm) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, r.keyPrefix+key)
}

func (r *RedisWarm) Scan(ctx context.Context, prefix string, limit int) ([]*Entry, error) {
	pattern := r.keyPrefix + prefix + "*"
	var entries []*Entry
	var cursor uint64

	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, int64(limit))
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			entry, err := r.Get(ctx, key[len(r.keyPrefix):])
			if err == nil && entry != nil {
				entries = append(entries, entry)
			}
			if len(entries) >= limit {
				return entries, nil
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}

	return entries, nil
}
