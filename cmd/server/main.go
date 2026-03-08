// WAOO Studio — AI Filmmaking Platform
//
// Main server entry point. Initializes all infrastructure, registers agents,
// and starts the HTTP API server.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redis "github.com/redis/go-redis/v9"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/character"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/director"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/location"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/media"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/storyboard"
	"github.com/uni-ai-studio/waoo-studio/internal/agents/voice"
	"github.com/uni-ai-studio/waoo-studio/internal/config"
	"github.com/uni-ai-studio/waoo-studio/internal/llm"
	"github.com/uni-ai-studio/waoo-studio/internal/memory"
	"github.com/uni-ai-studio/waoo-studio/internal/natsbus"
	"github.com/uni-ai-studio/waoo-studio/internal/pipeline"
	"github.com/uni-ai-studio/waoo-studio/internal/poller"
	"github.com/uni-ai-studio/waoo-studio/internal/tools"
	"github.com/uni-ai-studio/waoo-studio/internal/webhook"
)

const version = "0.3.0"

func main() {
	// Structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("Uni AI Studio starting", "version", version, "pid", os.Getpid())

	// Load configuration
	cfg := config.Load()

	// Graceful shutdown context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- Initialize infrastructure ---

	// PostgreSQL Connection Pool
	dbPool, err := pgxpool.New(ctx, cfg.Database.DSN())
	if err != nil {
		logger.Warn("PostgreSQL not available, running without DB", "error", err)
		dbPool = nil
	} else {
		defer dbPool.Close()
		logger.Info("PostgreSQL connected", "host", cfg.Database.Host, "db", cfg.Database.Database)

		// Run world state initialization
		initWorldState(ctx, dbPool, logger)
	}

	// NATS Message Bus
	bus, err := natsbus.New(cfg.NATS, logger)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	// LLM Router
	router := llm.NewRouter(cfg.LLM, logger)

	// Tool Registry — wraps all external AI providers
	toolRegistry := tools.NewRegistry(tools.ProviderKeys{
		FALKey:     cfg.Media.FALKey,
		ArkKey:     cfg.Media.ArkKey,
		MiniMaxKey: cfg.Media.MiniMaxKey,
		ViduKey:    cfg.Media.ViduKey,
		GoogleKey:  cfg.LLM.GoogleAIKey,
	}, logger)

	// Tiered Memory (hot + warm Redis + cold PostgreSQL)
	var warmBackend memory.WarmBackend
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis not available, running without warm memory", "error", err)
	} else {
		warmBackend = memory.NewRedisWarm(&redisClientAdapter{redisClient})
		logger.Info("Redis connected", "host", cfg.Redis.Host, "port", cfg.Redis.Port)
	}

	var memStore *memory.Store
	if dbPool != nil {
		coldBackend := memory.NewPGCold(dbPool)
		memStore = memory.NewStore(warmBackend, coldBackend, logger)
	} else {
		memStore = memory.NewStore(warmBackend, nil, logger)
	}
	_ = memStore // available for agents in future iterations

	// Async Task Poller with persistence
	var taskPersist *poller.PersistentStore
	if dbPool != nil {
		taskPersist = poller.NewPersistentStore(dbPool)
	}
	taskPoller := poller.NewPoller(func(task poller.TrackedTask) {
		logger.Info("generation task completed",
			"externalId", task.ExternalID,
			"provider", task.Provider,
			"resultUrl", task.ResultURL,
		)
		if taskPersist != nil {
			if task.Status == poller.TaskCompleted {
				_ = taskPersist.MarkCompleted(context.Background(), task.ExternalID, task.ResultURL)
			} else {
				_ = taskPersist.MarkFailed(context.Background(), task.ExternalID, task.Error)
			}
		}
	}, 5*time.Second, logger)

	// Recover pending tasks from DB on startup
	if taskPersist != nil {
		pending, err := taskPersist.LoadPending(ctx)
		if err == nil && len(pending) > 0 {
			for _, t := range pending {
				taskPoller.Track(t)
			}
			logger.Info("recovered pending tasks", "count", len(pending))
		}
	}
	taskPoller.Start(ctx)

	// Webhook handler for external callbacks
	webhookHandler := webhook.NewHandler(os.Getenv("WEBHOOK_SECRET"), logger)
	webhookHandler.OnComplete(func(event webhook.CompletionEvent) {
		logger.Info("webhook received",
			"provider", event.Provider,
			"externalId", event.ExternalID,
			"status", event.Status,
		)
		if taskPersist != nil && event.Status == "completed" {
			_ = taskPersist.MarkCompleted(context.Background(), event.ExternalID, event.ResultURL)
		}
	})

	// Filmmaking Pipeline (with checkpoint support)
	filmPipeline := pipeline.NewPipeline(bus, logger)
	var checkpointStore *pipeline.CheckpointStore
	if dbPool != nil {
		checkpointStore = pipeline.NewCheckpointStore(dbPool, logger)
		filmPipeline.SetCheckpointStore(checkpointStore)
	}
	if cfg.Media.FALKey != "" {
		filmPipeline.SetFALKey(cfg.Media.FALKey)
		logger.Info("FAL media generation enabled")
	}

	// Agent Supervisor — monitors health, tracks error rates
	supervisor := agent.NewSupervisor(logger)

	// --- Register all agents ---
	registry := agent.NewRegistry(bus, supervisor, logger)

	allAgents := []agent.Agent{
		director.New(bus, router, toolRegistry, logger),
		character.New(bus, router, toolRegistry, logger),
		location.New(bus, router, toolRegistry, logger),
		storyboard.New(bus, router, toolRegistry, logger),
		media.New(bus, router, toolRegistry, logger),
		voice.New(bus, router, toolRegistry, logger),
	}

	for _, a := range allAgents {
		if err := registry.Register(a); err != nil {
			logger.Error("failed to register agent", "agent", a.Name(), "error", err)
			os.Exit(1)
		}
		supervisor.Watch(a)
	}

	// Start all agents (subscribe to NATS)
	if err := registry.StartAll(ctx); err != nil {
		logger.Error("failed to start agents", "error", err)
		os.Exit(1)
	}
	supervisor.Start(ctx)
	logger.Info("all agents started", "count", len(allAgents))

	// --- HTTP API ---
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	mux := http.NewServeMux()

	// Root — API info
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "uni-ai-studio",
			"version": version,
			"endpoints": []string{
				"GET  /health",
				"GET  /agents",
				"GET  /agents/health",
				"GET  /agents/{name}",
				"POST /agents/{name}/send",
				"GET  /tools",
				"POST /pipeline/start",
				"GET  /pipeline/progress/{projectId}",
				"POST /webhooks/{provider}",
				"GET  /settings/llm",
				"PUT  /settings/llm",
			},
		})
	})

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		dbStatus := "disconnected"
		if dbPool != nil {
			if err := dbPool.Ping(context.Background()); err == nil {
				dbStatus = "connected"
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"service":   "uni-ai-studio",
			"version":   version,
			"database":  dbStatus,
			"pending":   taskPoller.PendingCount(),
		})
	})

	// List all agent cards (A2A discovery)
	mux.HandleFunc("GET /agents", func(w http.ResponseWriter, _ *http.Request) {
		cards := registry.List()
		writeJSON(w, http.StatusOK, map[string]any{
			"agents": cards,
			"count":  len(cards),
		})
	})

	// Agent health report (Supervisor)
	mux.HandleFunc("GET /agents/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"health": supervisor.HealthReport(),
		})
	})

	// Get specific agent card
	mux.HandleFunc("GET /agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		a, ok := registry.Get(name)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "agent not found"})
			return
		}
		writeJSON(w, http.StatusOK, a.Card())
	})

	// Send message to agent (A2A message/send)
	mux.HandleFunc("POST /agents/{name}/send", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		a, ok := registry.Get(name)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "agent not found"})
			return
		}

		var msg agent.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		msg.To = name
		msg.Timestamp = time.Now()

		result, err := a.HandleMessage(r.Context(), msg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, result)
	})

	// List tools
	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, _ *http.Request) {
		toolList := toolRegistry.List()
		writeJSON(w, http.StatusOK, map[string]any{
			"tools": toolList,
			"count": len(toolList),
		})
	})

	// Start filmmaking pipeline
	mux.HandleFunc("POST /pipeline/start", func(w http.ResponseWriter, r *http.Request) {
		var req pipeline.PipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		// Run pipeline asynchronously
		go func() {
			result, err := filmPipeline.Run(context.Background(), req)
			if err != nil {
				logger.Error("pipeline failed", "projectId", req.ProjectID, "error", err)
				return
			}
			logger.Info("pipeline completed", "projectId", result.ProjectID)
		}()

		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "started",
			"projectId": req.ProjectID,
			"message":   "Pipeline started in background",
		})
	})

	// Pipeline progress SSE
	mux.HandleFunc("GET /pipeline/progress/{projectId}", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		projectID := r.PathValue("projectId")
		events := make(chan pipeline.ProgressEvent, 10)

		filmPipeline.OnProgress(func(event pipeline.ProgressEvent) {
			if event.ProjectID == projectID {
				select {
				case events <- event:
				default: // Channel full, skip
				}
			}
		})

		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-events:
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()

				if event.Stage == pipeline.StageComplete || event.Status == "failed" {
					return
				}
			case <-time.After(30 * time.Second):
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	// Webhook endpoint for external provider callbacks
	mux.Handle("POST /webhooks/{provider}", webhookHandler)

	// --- Settings API ---

	// GET /settings/llm — return current LLM config (masked keys)
	mux.HandleFunc("GET /settings/llm", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, router.GetConfig())
	})

	// PUT /settings/llm — update runtime LLM config
	mux.HandleFunc("PUT /settings/llm", func(w http.ResponseWriter, r *http.Request) {
		var update llm.LLMSettingsJSON
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		router.UpdateConfig(update)
		writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
	})

	// OPTIONS handler for CORS preflight on settings
	mux.HandleFunc("OPTIONS /settings/llm", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /pipeline/{id} — get all stages for a project
	mux.HandleFunc("GET /pipeline/{id}", func(w http.ResponseWriter, r *http.Request) {
		if checkpointStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database not available"})
			return
		}
		projectID := r.PathValue("id")
		stages, err := checkpointStore.GetAllStages(r.Context(), projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"projectId": projectID,
			"stages":    stages,
		})
	})

	// PATCH /pipeline/{id}/stage/{stage}/output — overwrite stage output JSON
	mux.HandleFunc("PATCH /pipeline/{id}/stage/{stage}/output", func(w http.ResponseWriter, r *http.Request) {
		if checkpointStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database not available"})
			return
		}
		projectID := r.PathValue("id")
		stage := r.PathValue("stage")
		var output map[string]any
		if err := json.NewDecoder(r.Body).Decode(&output); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if err := checkpointStore.UpdateStageOutput(r.Context(), projectID, pipeline.Stage(stage), output); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
	})

	// POST /pipeline/{id}/stage/{stage}/media — add external media URL to stage output
	mux.HandleFunc("POST /pipeline/{id}/stage/{stage}/media", func(w http.ResponseWriter, r *http.Request) {
		if checkpointStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database not available"})
			return
		}
		projectID := r.PathValue("id")
		stage := r.PathValue("stage")
		var body struct {
			URL      string `json:"url"`
			Label    string `json:"label"`
			MimeType string `json:"mimeType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url is required"})
			return
		}
		// Build output map with injected media URL
		output := map[string]any{
			"userMedia": map[string]any{
				"url":      body.URL,
				"label":    body.Label,
				"mimeType": body.MimeType,
			},
		}
		if err := checkpointStore.UpdateStageOutput(r.Context(), projectID, pipeline.Stage(stage), output); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "media added", "url": body.URL})
	})

	// GET /projects — list all projects from pipeline_stages summary view
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		if dbPool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database not available"})
			return
		}
		rows, err := dbPool.Query(r.Context(), `
			SELECT project_id, total_stages, completed_stages, failed_stages,
			       started_at, finished_at, last_updated, overall_status
			FROM project_pipeline_summary
			ORDER BY last_updated DESC
			LIMIT 100`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		defer rows.Close()

		type ProjectSummary struct {
			ProjectID       string     `json:"projectId"`
			TotalStages     int        `json:"totalStages"`
			CompletedStages int        `json:"completedStages"`
			FailedStages    int        `json:"failedStages"`
			StartedAt       *time.Time `json:"startedAt,omitempty"`
			FinishedAt      *time.Time `json:"finishedAt,omitempty"`
			LastUpdated     time.Time  `json:"lastUpdated"`
			OverallStatus   string     `json:"overallStatus"`
		}

		var projects []ProjectSummary
		for rows.Next() {
			var p ProjectSummary
			if err := rows.Scan(&p.ProjectID, &p.TotalStages, &p.CompletedStages,
				&p.FailedStages, &p.StartedAt, &p.FinishedAt, &p.LastUpdated, &p.OverallStatus); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			projects = append(projects, p)
		}
		if err := rows.Err(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"projects": projects,
			"count":    len(projects),
		})
	})

	server := &http.Server{
		Addr:         addr,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: 0, // Disable for SSE
	}

	// Start server
	go func() {
		logger.Info("HTTP server starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	taskPoller.Stop()
	supervisor.Stop()

	if err := registry.StopAll(); err != nil {
		logger.Error("agent shutdown error", "error", err)
	}

	logger.Info("Uni AI Studio stopped")
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// corsMiddleware adds CORS headers to all responses.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// redisClientAdapter adapts go-redis Client to memory.RedisClient interface.
type redisClientAdapter struct {
	client *redis.Client
}

func (a *redisClientAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

func (a *redisClientAdapter) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	return a.client.Set(ctx, key, value, expiration).Err()
}

func (a *redisClientAdapter) Del(ctx context.Context, keys ...string) error {
	return a.client.Del(ctx, keys...).Err()
}

func (a *redisClientAdapter) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	return a.client.Scan(ctx, cursor, match, count).Result()
}

// initWorldState ensures essential DB tables exist and runs migrations.
func initWorldState(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		logger.Error("DB ping failed during init", "error", err)
		return
	}

	// Check for essential tables
	essentialTables := []string{
		"world_states", "world_events", "agent_decisions",
		"workflow_runs", "workflow_steps", "workflow_checkpoints",
	}

	for _, table := range essentialTables {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table,
		).Scan(&exists)
		if err != nil || !exists {
			logger.Warn("table missing, run migrations", "table", table)
		}
	}

	logger.Info("world state initialized")
}
