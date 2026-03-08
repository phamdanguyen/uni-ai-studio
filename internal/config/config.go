// Package config manages application configuration from environment variables and YAML files.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig
	NATS     NATSConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Storage  StorageConfig
	LLM      LLMConfig
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// NATSConfig holds NATS JetStream settings.
type NATSConfig struct {
	URL            string
	ClusterID      string
	MaxReconnects  int
	ReconnectWait  time.Duration
	RequestTimeout time.Duration
}

// DatabaseConfig holds PostgreSQL settings.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
	MaxConns int
}

// RedisConfig holds Redis settings.
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// StorageConfig holds S3-compatible object storage settings.
type StorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	OpenRouterAPIKey string
	OpenRouterBaseURL string
	GoogleAIKey      string
	AnthropicKey     string

	// Model routing
	FlashModel    string
	StandardModel string
	PremiumModel  string

	// Budget
	DefaultBudgetUSD float64
	RequestTimeoutS  int
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Database, d.SSLMode,
	)
}

// Load reads configuration from environment variables with defaults.
func Load() Config {
	return Config{
		Server: ServerConfig{
			Host:         envStr("SERVER_HOST", "0.0.0.0"),
			Port:         envInt("SERVER_PORT", 8080),
			ReadTimeout:  envDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: envDuration("SERVER_WRITE_TIMEOUT", 60*time.Second),
		},
		NATS: NATSConfig{
			URL:            envStr("NATS_URL", "nats://localhost:4222"),
			ClusterID:      envStr("NATS_CLUSTER_ID", "waoo-studio"),
			MaxReconnects:  envInt("NATS_MAX_RECONNECTS", 60),
			ReconnectWait:  envDuration("NATS_RECONNECT_WAIT", 2*time.Second),
			RequestTimeout: envDuration("NATS_REQUEST_TIMEOUT", 30*time.Second),
		},
		Database: DatabaseConfig{
			Host:     envStr("DB_HOST", "localhost"),
			Port:     envInt("DB_PORT", 5432),
			User:     envStr("DB_USER", "waoo"),
			Password: envStr("DB_PASSWORD", "waoo_secret"),
			Database: envStr("DB_NAME", "waoo_studio"),
			SSLMode:  envStr("DB_SSLMODE", "disable"),
			MaxConns: envInt("DB_MAX_CONNS", 20),
		},
		Redis: RedisConfig{
			Host:     envStr("REDIS_HOST", "localhost"),
			Port:     envInt("REDIS_PORT", 6379),
			Password: envStr("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		Storage: StorageConfig{
			Endpoint:  envStr("S3_ENDPOINT", "localhost:9000"),
			AccessKey: envStr("S3_ACCESS_KEY", "minioadmin"),
			SecretKey: envStr("S3_SECRET_KEY", "minioadmin"),
			Bucket:    envStr("S3_BUCKET", "waoo-media"),
			Region:    envStr("S3_REGION", "us-east-1"),
			UseSSL:    envBool("S3_USE_SSL", false),
		},
		LLM: LLMConfig{
			OpenRouterAPIKey:  envStr("OPENROUTER_API_KEY", ""),
			OpenRouterBaseURL: envStr("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
			GoogleAIKey:       envStr("GOOGLE_AI_KEY", ""),
			AnthropicKey:      envStr("ANTHROPIC_KEY", ""),
			FlashModel:        envStr("LLM_FLASH_MODEL", "google/gemini-2.0-flash-exp"),
			StandardModel:     envStr("LLM_STANDARD_MODEL", "anthropic/claude-sonnet-4-20250514"),
			PremiumModel:      envStr("LLM_PREMIUM_MODEL", "anthropic/claude-opus-4-20250514"),
			DefaultBudgetUSD:  envFloat("LLM_DEFAULT_BUDGET_USD", 10.0),
			RequestTimeoutS:   envInt("LLM_REQUEST_TIMEOUT_S", 120),
		},
	}
}

func envStr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func envFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
