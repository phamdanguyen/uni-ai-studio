package agent

import (
	"testing"
)

func TestBlackboard_SetAndGet(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "characters", "hero", "John")

	got, ok := bb.Get("characters", "hero")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if got != "John" {
		t.Fatalf("expected 'John', got %v", got)
	}
}

func TestBlackboard_GetMissingSectionReturnsNilFalse(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")

	got, ok := bb.Get("nonexistent", "key")
	if ok {
		t.Fatal("expected ok = false for missing section")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestBlackboard_GetSectionReturnsDeepCopy(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "chars", "name", "Alice")

	snapshot := bb.GetSection("chars")
	snapshot["name"] = "Bob" // mutate the copy

	got, _ := bb.Get("chars", "name")
	if got != "Alice" {
		t.Fatalf("expected original value 'Alice', got %v (mutation safety broken)", got)
	}
}

func TestBlackboard_MergeAdditive(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "data", "existing", "value1")
	bb.Merge("agent-b", "data", map[string]any{"new": "value2"})

	// existing key should still be there
	got, ok := bb.Get("data", "existing")
	if !ok || got != "value1" {
		t.Fatalf("expected existing key to remain, got %v, ok=%v", got, ok)
	}

	// new key should be added
	got2, ok2 := bb.Get("data", "new")
	if !ok2 || got2 != "value2" {
		t.Fatalf("expected merged key, got %v, ok=%v", got2, ok2)
	}
}

func TestBlackboard_SnapshotDeepCopy(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "sec1", "k1", "v1")

	snap := bb.Snapshot()
	snap["sec1"]["k1"] = "mutated"

	got, _ := bb.Get("sec1", "k1")
	if got != "v1" {
		t.Fatalf("expected original 'v1', got %v (snapshot not deep copy)", got)
	}
}

func TestBlackboard_HistoryRecordsOperations(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "s1", "k1", "v1")
	bb.Merge("agent-b", "s1", map[string]any{"k2": "v2"})

	history := bb.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].Action != "set" {
		t.Errorf("expected first action = 'set', got %q", history[0].Action)
	}
	if history[1].Action != "merge" {
		t.Errorf("expected second action = 'merge', got %q", history[1].Action)
	}
}

func TestBlackboard_SectionVersionIncrementsOnSetAndMerge(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")

	if v := bb.SectionVersion("s1"); v != 0 {
		t.Fatalf("expected version 0 for nonexistent section, got %d", v)
	}

	bb.Set("agent-a", "s1", "k1", "v1")
	if v := bb.SectionVersion("s1"); v != 1 {
		t.Fatalf("expected version 1 after Set, got %d", v)
	}

	bb.Merge("agent-b", "s1", map[string]any{"k2": "v2"})
	if v := bb.SectionVersion("s1"); v != 2 {
		t.Fatalf("expected version 2 after Merge, got %d", v)
	}
}

func TestBlackboard_OnChangeListenerFires(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	var received []Change

	bb.OnChange(func(c Change) {
		received = append(received, c)
	})

	bb.Set("agent-a", "s1", "k1", "v1")
	bb.Merge("agent-b", "s1", map[string]any{"k2": "v2"})

	if len(received) != 2 {
		t.Fatalf("expected 2 change events, got %d", len(received))
	}
	if received[0].Agent != "agent-a" {
		t.Errorf("expected first change agent = 'agent-a', got %q", received[0].Agent)
	}
	if received[1].Agent != "agent-b" {
		t.Errorf("expected second change agent = 'agent-b', got %q", received[1].Agent)
	}
}

func TestBlackboard_SetOwnerAndCheckOwnership(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")
	bb.Set("agent-a", "s1", "k1", "v1") // create section
	bb.SetOwner("s1", "agent-a")

	// Owner can write
	if err := bb.CheckOwnership("s1", "agent-a"); err != nil {
		t.Fatalf("expected owner to pass check, got %v", err)
	}

	// Non-owner should fail
	if err := bb.CheckOwnership("s1", "agent-b"); err == nil {
		t.Fatal("expected non-owner to fail ownership check")
	}
}

func TestBlackboard_CheckOwnershipNewSectionReturnsNil(t *testing.T) {
	bb := NewBlackboard("proj-1", "run-1")

	// New section (doesn't exist) → anyone can write
	if err := bb.CheckOwnership("new-section", "anyone"); err != nil {
		t.Fatalf("expected nil for new section, got %v", err)
	}
}
