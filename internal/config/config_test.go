package config

import (
	"testing"
	"time"
)

func TestDSN(t *testing.T) {
	db := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "waoo",
		Password: "secret",
		Database: "waoo_studio",
		SSLMode:  "disable",
	}

	got := db.DSN()
	want := "postgres://waoo:secret@localhost:5432/waoo_studio?sslmode=disable"
	if got != want {
		t.Fatalf("DSN() = %q, want %q", got, want)
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host '0.0.0.0', got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected default DB host 'localhost', got %q", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("expected default DB port 5432, got %d", cfg.Database.Port)
	}
	if cfg.LLM.DefaultBudgetUSD != 10.0 {
		t.Errorf("expected default budget 10.0, got %f", cfg.LLM.DefaultBudgetUSD)
	}
	if cfg.LLM.RequestTimeoutS != 120 {
		t.Errorf("expected default timeout 120, got %d", cfg.LLM.RequestTimeoutS)
	}
}

func TestLoad_WithEnvOverrides(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("LLM_DEFAULT_BUDGET_USD", "25.5")
	t.Setenv("AUTH_ENABLED", "true")

	cfg := Load()

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("expected DB host 'db.example.com', got %q", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("expected DB port 5433, got %d", cfg.Database.Port)
	}
	if cfg.LLM.DefaultBudgetUSD != 25.5 {
		t.Errorf("expected budget 25.5, got %f", cfg.LLM.DefaultBudgetUSD)
	}
	if !cfg.AuthEnabled {
		t.Error("expected AuthEnabled = true")
	}
}

// --- envStr tests ---

func TestEnvStr_Present(t *testing.T) {
	t.Setenv("TEST_STR", "hello")
	got := envStr("TEST_STR", "default")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestEnvStr_Absent(t *testing.T) {
	got := envStr("NONEXISTENT_STR_KEY_12345", "default")
	if got != "default" {
		t.Fatalf("expected 'default', got %q", got)
	}
}

// --- envInt tests ---

func TestEnvInt_Valid(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	got := envInt("TEST_INT", 0)
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestEnvInt_Invalid(t *testing.T) {
	t.Setenv("TEST_INT_BAD", "not-a-number")
	got := envInt("TEST_INT_BAD", 99)
	if got != 99 {
		t.Fatalf("expected default 99 for invalid int, got %d", got)
	}
}

func TestEnvInt_Absent(t *testing.T) {
	got := envInt("NONEXISTENT_INT_KEY_12345", 99)
	if got != 99 {
		t.Fatalf("expected default 99, got %d", got)
	}
}

// --- envFloat tests ---

func TestEnvFloat_Valid(t *testing.T) {
	t.Setenv("TEST_FLOAT", "3.14")
	got := envFloat("TEST_FLOAT", 0)
	if got != 3.14 {
		t.Fatalf("expected 3.14, got %f", got)
	}
}

func TestEnvFloat_Invalid(t *testing.T) {
	t.Setenv("TEST_FLOAT_BAD", "abc")
	got := envFloat("TEST_FLOAT_BAD", 1.5)
	if got != 1.5 {
		t.Fatalf("expected default 1.5, got %f", got)
	}
}

// --- envBool tests ---

func TestEnvBool_Valid(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")
	got := envBool("TEST_BOOL", false)
	if !got {
		t.Fatal("expected true")
	}
}

func TestEnvBool_Invalid(t *testing.T) {
	t.Setenv("TEST_BOOL_BAD", "maybe")
	got := envBool("TEST_BOOL_BAD", false)
	if got {
		t.Fatal("expected default false for invalid bool")
	}
}

// --- envDuration tests ---

func TestEnvDuration_Valid(t *testing.T) {
	t.Setenv("TEST_DUR", "5s")
	got := envDuration("TEST_DUR", time.Second)
	if got != 5*time.Second {
		t.Fatalf("expected 5s, got %v", got)
	}
}

func TestEnvDuration_Invalid(t *testing.T) {
	t.Setenv("TEST_DUR_BAD", "not-duration")
	got := envDuration("TEST_DUR_BAD", 10*time.Second)
	if got != 10*time.Second {
		t.Fatalf("expected default 10s, got %v", got)
	}
}

func TestEnvDuration_Absent(t *testing.T) {
	got := envDuration("NONEXISTENT_DUR_KEY_12345", 30*time.Second)
	if got != 30*time.Second {
		t.Fatalf("expected default 30s, got %v", got)
	}
}
