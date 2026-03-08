// Package agent — Blackboard pattern for shared state between agents.
// During a pipeline run, multiple agents need to read/write shared context
// (characters, locations, storyboard panels). The Blackboard provides
// concurrent-safe access to this shared state.
package agent

import (
	"fmt"
	"sync"
	"time"
)

// Blackboard is a shared workspace for agents collaborating on a pipeline run.
// It acts as a structured scratchpad where agents read/write data by namespace.
type Blackboard struct {
	mu        sync.RWMutex
	projectID string
	runID     string
	sections  map[string]*Section
	history   []Change
	listeners []ChangeListener
}

// Section is a named area of the blackboard (e.g., "characters", "locations").
type Section struct {
	Name      string         `json:"name"`
	Data      map[string]any `json:"data"`
	Owner     string         `json:"owner"` // Agent that owns this section
	Version   int            `json:"version"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Change records a modification to the blackboard.
type Change struct {
	Section   string    `json:"section"`
	Key       string    `json:"key"`
	Agent     string    `json:"agent"`
	Action    string    `json:"action"` // "set", "delete", "merge"
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
}

// ChangeListener is called when the blackboard is modified.
type ChangeListener func(change Change)

// NewBlackboard creates a new shared blackboard for a pipeline run.
func NewBlackboard(projectID, runID string) *Blackboard {
	return &Blackboard{
		projectID: projectID,
		runID:     runID,
		sections:  make(map[string]*Section),
	}
}

// OnChange registers a listener for blackboard modifications.
func (b *Blackboard) OnChange(listener ChangeListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, listener)
}

// Set writes a value to a section, creating the section if needed.
func (b *Blackboard) Set(agentName, section, key string, value any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sec := b.getOrCreateSection(section)
	sec.Data[key] = value
	sec.Version++
	sec.UpdatedAt = time.Now()

	change := Change{
		Section:   section,
		Key:       key,
		Agent:     agentName,
		Action:    "set",
		Version:   sec.Version,
		Timestamp: time.Now(),
	}
	b.history = append(b.history, change)

	for _, l := range b.listeners {
		l(change)
	}
}

// Get reads a value from a section.
func (b *Blackboard) Get(section, key string) (any, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sec, ok := b.sections[section]
	if !ok {
		return nil, false
	}
	val, exists := sec.Data[key]
	return val, exists
}

// GetSection returns all data in a section.
func (b *Blackboard) GetSection(section string) map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sec, ok := b.sections[section]
	if !ok {
		return nil
	}

	// Return a copy
	result := make(map[string]any, len(sec.Data))
	for k, v := range sec.Data {
		result[k] = v
	}
	return result
}

// Merge merges data into a section (additive, doesn't delete existing keys).
func (b *Blackboard) Merge(agentName, section string, data map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sec := b.getOrCreateSection(section)
	for k, v := range data {
		sec.Data[k] = v
	}
	sec.Version++
	sec.UpdatedAt = time.Now()

	change := Change{
		Section:   section,
		Key:       "*",
		Agent:     agentName,
		Action:    "merge",
		Version:   sec.Version,
		Timestamp: time.Now(),
	}
	b.history = append(b.history, change)

	for _, l := range b.listeners {
		l(change)
	}
}

// Snapshot returns a read-only snapshot of the entire blackboard.
func (b *Blackboard) Snapshot() map[string]map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snap := make(map[string]map[string]any, len(b.sections))
	for name, sec := range b.sections {
		data := make(map[string]any, len(sec.Data))
		for k, v := range sec.Data {
			data[k] = v
		}
		snap[name] = data
	}
	return snap
}

// History returns the change log.
func (b *Blackboard) History() []Change {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Change, len(b.history))
	copy(result, b.history)
	return result
}

// SectionVersion returns the current version of a section.
func (b *Blackboard) SectionVersion(section string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sec, ok := b.sections[section]
	if !ok {
		return 0
	}
	return sec.Version
}

// SetOwner marks which agent owns a section (for conflict resolution).
func (b *Blackboard) SetOwner(section, agentName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sec := b.getOrCreateSection(section)
	sec.Owner = agentName
}

// CheckOwnership verifies an agent has write access to a section.
func (b *Blackboard) CheckOwnership(section, agentName string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sec, ok := b.sections[section]
	if !ok {
		return nil // New section, anyone can write
	}
	if sec.Owner != "" && sec.Owner != agentName {
		return fmt.Errorf("section %q owned by %s, %s cannot write", section, sec.Owner, agentName)
	}
	return nil
}

func (b *Blackboard) getOrCreateSection(name string) *Section {
	sec, ok := b.sections[name]
	if !ok {
		sec = &Section{
			Name:      name,
			Data:      make(map[string]any),
			UpdatedAt: time.Now(),
		}
		b.sections[name] = sec
	}
	return sec
}
