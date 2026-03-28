package prompts

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// --- Render tests ---

func TestRender_SingleVar(t *testing.T) {
	got := Render("Hello {name}!", map[string]string{"name": "World"})
	want := "Hello World!"
	if got != want {
		t.Fatalf("Render single var = %q, want %q", got, want)
	}
}

func TestRender_MultipleVars(t *testing.T) {
	tmpl := "{greeting}, {name}! Welcome to {place}."
	got := Render(tmpl, map[string]string{
		"greeting": "Hello",
		"name":     "Alice",
		"place":    "Wonderland",
	})
	want := "Hello, Alice! Welcome to Wonderland."
	if got != want {
		t.Fatalf("Render multiple vars = %q, want %q", got, want)
	}
}

func TestRender_MissingVarLeftAsIs(t *testing.T) {
	got := Render("Hello {name}, {unknown}!", map[string]string{"name": "Bob"})
	want := "Hello Bob, {unknown}!"
	if got != want {
		t.Fatalf("Render missing var = %q, want %q", got, want)
	}
}

func TestRender_EmptyVars(t *testing.T) {
	got := Render("Hello {name}!", map[string]string{})
	want := "Hello {name}!"
	if got != want {
		t.Fatalf("Render empty vars = %q, want %q", got, want)
	}
}

func TestRender_NoPlaceholders(t *testing.T) {
	got := Render("No placeholders here.", map[string]string{"key": "value"})
	want := "No placeholders here."
	if got != want {
		t.Fatalf("Render no placeholders = %q, want %q", got, want)
	}
}

// --- cacheKey tests ---

func TestCacheKey(t *testing.T) {
	got := cacheKey("director", "analyze", "en")
	want := "director/analyze.en"
	if got != want {
		t.Fatalf("cacheKey = %q, want %q", got, want)
	}
}

// --- Load tests ---

func TestLoad_ValidFile(t *testing.T) {
	// Clear the cache to avoid interference from other tests
	cache = sync.Map{}

	// Create a temp prompt file
	dir := t.TempDir()
	catDir := filepath.Join(dir, "test-category")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "greeting.en.txt"), []byte("  Hello {name}!  "), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override promptsDir for this test
	origDir := promptsDir
	promptsDir = dir
	t.Cleanup(func() { promptsDir = origDir; cache = sync.Map{} })

	content, err := Load("test-category", "greeting", "en")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if content != "Hello {name}!" { // trimmed
		t.Fatalf("Load = %q, want 'Hello {name}!'", content)
	}
}

func TestLoad_CacheHit(t *testing.T) {
	cache = sync.Map{}

	// Create a temp file
	dir := t.TempDir()
	catDir := filepath.Join(dir, "cached")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "test.en.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir := promptsDir
	promptsDir = dir
	t.Cleanup(func() { promptsDir = origDir; cache = sync.Map{} })

	// First load
	first, err := Load("cached", "test", "en")
	if err != nil {
		t.Fatal(err)
	}

	// Change the file content
	if err := os.WriteFile(filepath.Join(catDir, "test.en.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second load should return cached value
	second, err := Load("cached", "test", "en")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("expected cache hit (same value), first=%q, second=%q", first, second)
	}
}

func TestLoad_MissingFileReturnsError(t *testing.T) {
	cache = sync.Map{}

	origDir := promptsDir
	promptsDir = t.TempDir()
	t.Cleanup(func() { promptsDir = origDir; cache = sync.Map{} })

	_, err := Load("nonexistent", "missing", "en")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
