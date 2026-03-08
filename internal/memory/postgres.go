// Package memory — PostgreSQL cold backend implementation.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGCold implements ColdBackend using PostgreSQL.
type PGCold struct {
	pool *pgxpool.Pool
}

// NewPGCold creates a PostgreSQL-backed cold store.
// Requires the agent_memory table (see migration below).
func NewPGCold(pool *pgxpool.Pool) *PGCold {
	return &PGCold{pool: pool}
}

func (p *PGCold) Get(ctx context.Context, key string) (*Entry, error) {
	var valueJSON []byte
	var tier string
	var tags []string
	var createdAt, accessedAt time.Time
	var ttlSeconds *int

	err := p.pool.QueryRow(ctx,
		`SELECT value, tier, tags, ttl_seconds, created_at, accessed_at
		 FROM agent_memory WHERE key = $1`, key,
	).Scan(&valueJSON, &tier, &tags, &ttlSeconds, &createdAt, &accessedAt)

	if err != nil {
		return nil, nil // Not found
	}

	var value map[string]any
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		return nil, fmt.Errorf("unmarshal value: %w", err)
	}

	var ttl time.Duration
	if ttlSeconds != nil {
		ttl = time.Duration(*ttlSeconds) * time.Second
	}

	return &Entry{
		Key:        key,
		Value:      value,
		Tier:       Tier(tier),
		TTL:        ttl,
		CreatedAt:  createdAt,
		AccessedAt: accessedAt,
		Tags:       tags,
	}, nil
}

func (p *PGCold) Set(ctx context.Context, key string, entry *Entry) error {
	valueJSON, err := json.Marshal(entry.Value)
	if err != nil {
		return err
	}

	var ttlSeconds *int
	if entry.TTL > 0 {
		s := int(entry.TTL.Seconds())
		ttlSeconds = &s
	}

	_, err = p.pool.Exec(ctx,
		`INSERT INTO agent_memory (key, value, tier, tags, ttl_seconds, created_at, accessed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (key) DO UPDATE SET
		   value = EXCLUDED.value,
		   tier = EXCLUDED.tier,
		   tags = EXCLUDED.tags,
		   ttl_seconds = EXCLUDED.ttl_seconds,
		   accessed_at = EXCLUDED.accessed_at`,
		key, valueJSON, string(entry.Tier), entry.Tags,
		ttlSeconds, entry.CreatedAt, entry.AccessedAt,
	)
	return err
}

func (p *PGCold) Delete(ctx context.Context, key string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM agent_memory WHERE key = $1`, key)
	return err
}

func (p *PGCold) Query(ctx context.Context, tags []string, limit int) ([]*Entry, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT key, value, tier, tags, ttl_seconds, created_at, accessed_at
		 FROM agent_memory
		 WHERE tags && $1
		 ORDER BY accessed_at DESC
		 LIMIT $2`, tags, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*Entry
	for rows.Next() {
		var key string
		var valueJSON []byte
		var tier string
		var entryTags []string
		var ttlSeconds *int
		var createdAt, accessedAt time.Time

		if err := rows.Scan(&key, &valueJSON, &tier, &entryTags, &ttlSeconds, &createdAt, &accessedAt); err != nil {
			return nil, err
		}

		var value map[string]any
		if err := json.Unmarshal(valueJSON, &value); err != nil {
			continue
		}

		var ttl time.Duration
		if ttlSeconds != nil {
			ttl = time.Duration(*ttlSeconds) * time.Second
		}

		entries = append(entries, &Entry{
			Key: key, Value: value, Tier: Tier(tier),
			TTL: ttl, CreatedAt: createdAt, AccessedAt: accessedAt, Tags: entryTags,
		})
	}

	return entries, nil
}
