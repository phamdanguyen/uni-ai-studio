// Package memory implements a tiered memory store for agents.
// Three tiers with different latency/capacity tradeoffs:
//   - Hot:  In-process map — current conversation context (~1ms)
//   - Warm: Redis — recent sessions, character profiles (~5ms)
//   - Cold: PostgreSQL — full project history, audit trail (~20ms)
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Tier identifies the storage tier.
type Tier string

const (
	TierHot  Tier = "hot"
	TierWarm Tier = "warm"
	TierCold Tier = "cold"
)

// Entry is a single memory item.
type Entry struct {
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	Tier      Tier           `json:"tier"`
	TTL       time.Duration  `json:"ttl,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	AccessedAt time.Time     `json:"accessedAt"`
	Tags      []string       `json:"tags,omitempty"`
}

// Store provides tiered read-through caching with automatic promotion/demotion.
type Store struct {
	hot    *hotStore
	warm   WarmBackend
	cold   ColdBackend
	logger *slog.Logger
}

// WarmBackend abstracts Redis or similar fast KV store.
type WarmBackend interface {
	Get(ctx context.Context, key string) (*Entry, error)
	Set(ctx context.Context, key string, entry *Entry) error
	Delete(ctx context.Context, key string) error
	Scan(ctx context.Context, prefix string, limit int) ([]*Entry, error)
}

// ColdBackend abstracts PostgreSQL or similar persistent store.
type ColdBackend interface {
	Get(ctx context.Context, key string) (*Entry, error)
	Set(ctx context.Context, key string, entry *Entry) error
	Delete(ctx context.Context, key string) error
	Query(ctx context.Context, tags []string, limit int) ([]*Entry, error)
}

// NewStore creates a tiered memory store.
// warm and cold can be nil for degraded operation.
func NewStore(warm WarmBackend, cold ColdBackend, logger *slog.Logger) *Store {
	return &Store{
		hot:    newHotStore(),
		warm:   warm,
		cold:   cold,
		logger: logger.With("component", "memory"),
	}
}

// Get reads from hot → warm → cold, promoting on hit.
func (s *Store) Get(ctx context.Context, key string) (*Entry, error) {
	// 1. Hot (in-memory)
	if entry := s.hot.get(key); entry != nil {
		return entry, nil
	}

	// 2. Warm (Redis)
	if s.warm != nil {
		entry, err := s.warm.Get(ctx, key)
		if err == nil && entry != nil {
			s.hot.set(key, entry) // Promote to hot
			return entry, nil
		}
	}

	// 3. Cold (PostgreSQL)
	if s.cold != nil {
		entry, err := s.cold.Get(ctx, key)
		if err == nil && entry != nil {
			// Promote to warm + hot
			if s.warm != nil {
				_ = s.warm.Set(ctx, key, entry)
			}
			s.hot.set(key, entry)
			return entry, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cold get: %w", err)
		}
	}

	return nil, nil // Not found
}

// Set writes to all available tiers.
func (s *Store) Set(ctx context.Context, key string, value map[string]any, opts ...SetOption) error {
	cfg := &setConfig{tier: TierHot}
	for _, opt := range opts {
		opt(cfg)
	}

	entry := &Entry{
		Key:        key,
		Value:      value,
		Tier:       cfg.tier,
		TTL:        cfg.ttl,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		Tags:       cfg.tags,
	}

	// Always write to hot
	s.hot.set(key, entry)

	// Write to warm if tier >= warm
	if cfg.tier != TierHot && s.warm != nil {
		if err := s.warm.Set(ctx, key, entry); err != nil {
			s.logger.Warn("warm set failed", "key", key, "error", err)
		}
	}

	// Write to cold if tier == cold
	if cfg.tier == TierCold && s.cold != nil {
		if err := s.cold.Set(ctx, key, entry); err != nil {
			return fmt.Errorf("cold set: %w", err)
		}
	}

	return nil
}

// Delete removes from all tiers.
func (s *Store) Delete(ctx context.Context, key string) error {
	s.hot.delete(key)
	if s.warm != nil {
		_ = s.warm.Delete(ctx, key)
	}
	if s.cold != nil {
		return s.cold.Delete(ctx, key)
	}
	return nil
}

// ProjectMemory returns a scoped view for a specific project.
func (s *Store) ProjectMemory(projectID string) *ProjectScope {
	return &ProjectScope{store: s, prefix: "project:" + projectID + ":"}
}

// AgentMemory returns a scoped view for a specific agent.
func (s *Store) AgentMemory(agentName, projectID string) *AgentScope {
	return &AgentScope{
		store:  s,
		prefix: fmt.Sprintf("agent:%s:%s:", agentName, projectID),
	}
}

// --- Set Options ---

type setConfig struct {
	tier Tier
	ttl  time.Duration
	tags []string
}

type SetOption func(*setConfig)

func WithTier(tier Tier) SetOption {
	return func(c *setConfig) { c.tier = tier }
}

func WithTTL(ttl time.Duration) SetOption {
	return func(c *setConfig) { c.ttl = ttl }
}

func WithTags(tags ...string) SetOption {
	return func(c *setConfig) { c.tags = tags }
}

// --- Hot Store (in-process) ---

type hotStore struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	maxSize int
}

func newHotStore() *hotStore {
	return &hotStore{
		entries: make(map[string]*Entry),
		maxSize: 10000,
	}
}

func (h *hotStore) get(key string) *Entry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	e, ok := h.entries[key]
	if !ok {
		return nil
	}
	if e.TTL > 0 && time.Since(e.CreatedAt) > e.TTL {
		return nil // Expired
	}
	e.AccessedAt = time.Now()
	return e
}

func (h *hotStore) set(key string, entry *Entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) >= h.maxSize {
		h.evictOldest()
	}
	h.entries[key] = entry
}

func (h *hotStore) delete(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.entries, key)
}

func (h *hotStore) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range h.entries {
		if oldestKey == "" || e.AccessedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.AccessedAt
		}
	}
	if oldestKey != "" {
		delete(h.entries, oldestKey)
	}
}

// --- Scoped Views ---

// ProjectScope provides project-scoped memory access.
type ProjectScope struct {
	store  *Store
	prefix string
}

func (p *ProjectScope) Get(ctx context.Context, key string) (*Entry, error) {
	return p.store.Get(ctx, p.prefix+key)
}

func (p *ProjectScope) Set(ctx context.Context, key string, value map[string]any, opts ...SetOption) error {
	return p.store.Set(ctx, p.prefix+key, value, opts...)
}

// AgentScope provides agent+project-scoped memory access.
type AgentScope struct {
	store  *Store
	prefix string
}

func (a *AgentScope) Get(ctx context.Context, key string) (*Entry, error) {
	return a.store.Get(ctx, a.prefix+key)
}

func (a *AgentScope) Set(ctx context.Context, key string, value map[string]any, opts ...SetOption) error {
	return a.store.Set(ctx, a.prefix+key, value, opts...)
}

func (a *AgentScope) SetJSON(ctx context.Context, key string, v any, opts ...SetOption) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	return a.Set(ctx, key, m, opts...)
}
