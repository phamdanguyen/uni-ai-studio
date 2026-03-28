# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WAOO Studio** – AI Filmmaking Platform. Backend Go + frontend Next.js, kiến trúc multi-agent (A2A-style messaging qua NATS) để chạy pipeline sản xuất phim từ story text đến media assembly.

## Source of Truth (quan trọng)

Khi nội dung trong file này lệch với code, ưu tiên đọc trực tiếp:

- API routes: `cmd/server/main.go`
- Runtime config/env defaults: `internal/config/config.go` + `.env.example`
- Infrastructure/services/ports: `docker-compose.yml`
- Agent contracts: `internal/agent/types.go`, `internal/agent/interfaces.go`
- Pipeline stages/modes: `internal/pipeline/`
- Prompt templates: `lib/prompts/`

## Commands

### Backend (Go) — chạy từ thư mục gốc

```bash
make dev          # go run ./cmd/server
make build        # Build: bin/waoo-server
make test         # go test -race -cover ./...
make lint         # golangci-lint run ./...
make deps         # go mod tidy
make generate     # go generate ./...
make clean        # rm -rf bin/
make help         # List all make targets
```

### Infrastructure

```bash
make infra        # docker compose up -d
make infra-down   # docker compose down
make infra-reset  # docker compose down -v && up -d
make migrate      # apply migrations/*.sql via docker exec to waoo-postgres
```

### Frontend — chạy từ `web/`

```bash
npm run dev
npm run build
npm run lint
npm run start
```

### Chạy một test cụ thể (Go)

```bash
go test -run TestFunctionName ./internal/path/to/package/...
```

> Lưu ý: hiện tại repo chưa có `*_test.go`; `make test` vẫn chạy toàn bộ package testable.

## High-level Architecture

### Backend

- Entry point: `cmd/server/main.go` (version `0.3.0`)
- Core flow startup:
  1. Load env config (`config.Load()`)
  2. PostgreSQL init (graceful degradation nếu fail) → `initWorldState()` nếu connected
  3. NATS init (**hard requirement**, fail thì exit)
  4. LLM router + tool registry + memory store + async poller + webhook handler
  5. Pipeline engines (Autopilot + Step-by-Step)
  6. Agent registry/supervisor (register 6 agents)
  7. HTTP server (`net/http`, `WriteTimeout: 0` cho SSE)

Dependency behavior:
- NATS là dependency bắt buộc.
- PostgreSQL/Redis unavailable → server vẫn chạy với degraded capabilities.

### Agent System

- Core interface: `internal/agent/types.go` (`Card`, `HandleMessage`, `HandleStream`, `Name`)
- Supporting interfaces: `internal/agent/interfaces.go` (`MessageBus`, `ModelRouter`, `ToolRegistry`, `Registry`)
- Shared base: `internal/agent/base.go` — helpers: `CallLLM`, `CallLLMWithJSON`, `AskAgent` (A2A request/reply), `NotifyAgent` (fire-and-forget), `UseTool`
- Collaboration: `internal/agent/collaboration.go` — Blackboard pattern (shared workspace, concurrent-safe sections, ownership/conflict resolution)
- Supervisor: `internal/agent/supervisor.go` — health tracking (healthy/degraded/failed), heartbeat staleness (60s), error rate thresholds, `ExecuteParallel()`, `ConflictResolver` (3 policies: last_write_wins, owner_priority, merge)
- NATS subject convention: `agent.{name}.{skillId}`
- Queue group per agent: `agent-{name}-workers`

Agents trong `internal/agents/`: director, character, location, storyboard, media, voice

### Pipeline

Stages (11):
`analysis → planning → characters → locations → segmentation → screenplay → storyboard → media_gen → quality_check → voice → assembly`

- `characters` và `locations` chạy song song trong autopilot flow.
- Hai mode:
  - **Autopilot** (`autopilot.go`): Publish 1 message đến Director (`start_production`), Director tự điều phối A2A. Subscribe NATS events để forward ra SSE.
  - **Step-by-Step** (`stepbystep.go`): Human-gated, state lưu hoàn toàn vào PostgreSQL, có checkpoint/retry/edit theo stage.
- Checkpoint persistence: `pipeline_checkpoints`, `pipeline_stages`, `pipeline_runs` tables
- Pipeline events published on NATS: `pipeline.{projectId}.events`

### LLM Router (`internal/llm/`)

- Tất cả LLM calls đi qua OpenRouter API (`/chat/completions`)
- Circuit breaker (`circuit.go`): maxFailures=5, recoveryTimeout=30s, halfOpenMax=2
- Rate limiter: token bucket 4 req/s, burst 8
- Per-agent model overrides: `agentModels map[string]map[ModelTier]string`
- Temperature theo tier: flash=0.3, standard=0.7, premium=0.5
- Gemma models: không dùng system role, merge system+user prompt
- Runtime settings API: `GetConfig()` mask API keys, `UpdateConfig()` skip masked values

### Memory (`internal/memory/`)

- Tiered: hot (in-process, maxSize=10000, LRU eviction, TTL) + warm (Redis, prefix `waoo:mem:`, TTL 30m) + cold (PostgreSQL, table `agent_memory`, tag queries)
- Read-through caching: Get hot → warm → cold, promote on hit
- Scoped views: `ProjectMemory(projectID)`, `AgentMemory(agentName, projectID)`
- `memStore` được inject vào tất cả agents qua `NewBaseAgent()`; truy cập qua `a.Memory()`

### Quality Gate (`internal/qualitygate/`)

- LLM-based quality evaluation: score 0.0-1.0 trên 4 dimensions (composition, consistency, technical, narrative)
- Retry loop: tự động refine prompt qua LLM rồi generate lại nếu score thấp

### World State (`internal/worldstate/`)

- Event-sourced shared state giữa agents với optimistic locking
- Agent decision audit trail
- Tables: `world_states`, `world_events`, `agent_decisions`, `workflow_runs`, `workflow_steps`, `workflow_checkpoints`

### Workflow Engine (`internal/workflow/`)

- Lightweight durable workflow (Temporal-inspired): DAG execution, checkpointing, resume, retry with backoff
- Engine initialized trong main.go khi DB available; sẵn sàng cho integration

### Prompt Templates (`lib/prompts/`)

- File-based prompt loader với template rendering và in-memory cache
- `Load(category, name, lang)`, `Render(template, vars)` — template syntax: `{variable_name}`
- 2 categories: `novel-promotion` (~25 prompts), `character-reference` (2 prompts)
- Multilingual: `.en.txt` và `.zh.txt`

### Tool Registry (`internal/tools/`)

13 registered tools:
- Image: `image_fal`, `image_ark`, `image_google`, `image_generator` (dispatcher)
- Video: `video_fal`, `video_ark`, `video_minimax`, `video_vidu`, `video_generator` (dispatcher)
- Audio: `tts_generator` (Qwen TTS), `voice_designer`, `lip_sync`
- Một số tools trả về "pending_implementation": `google_image`, `qwen_tts`, `lip_sync`

### Other subsystems

- Async task polling + persistence: `internal/poller/`
- NATS bus implementation: `internal/natsbus/bus.go`
- Provider webhook normalization: `internal/webhook/`
- Auth middleware (Keycloak JWT): `internal/auth/middleware.go` — toggle via `AUTH_ENABLED` env (default `false`)

### Frontend (Next.js)

- Stack: Next.js 16, React 19, TypeScript 5, Tailwind v4 (CSS-based config via `@theme inline`, không có `tailwind.config.*`)
- **Toàn bộ pages là `"use client"`** — không có server components hay server actions
- Output mode: `standalone` (cho Docker)
- Fonts: Plus Jakarta Sans + JetBrains Mono (Google Fonts CDN, không dùng `next/font`)
- Design: dark-only theme, accent color gold `#f5b240`, cinematic noise grain overlay
- Không có `components/` directory — tất cả components inline trong page files
- `web/lib/`: 4 files (`api.ts`, `keycloak.ts`, `sse.ts`, `utils.ts`)
- SSE: JWT token truyền qua query parameter (EventSource không hỗ trợ custom headers)
- Keycloak: `login-required`, PKCE S256, token refresh mỗi 60s

Routes: `/` (Dashboard), `/agents`, `/tools`, `/new`, `/project/[id]`, `/projects`, `/settings`

## API Surface

Xem đầy đủ trong `cmd/server/main.go`. Các nhóm chính:

- Health: `GET /health`, `GET /`
- Agents: `GET /agents`, `GET /agents/health`, `GET /agents/{name}`, `POST /agents/{name}/send`
- Tools: `GET /tools`
- Pipeline: `POST /pipeline/start`, `GET /pipeline/progress/{projectId}` (SSE)
- Pipeline stage ops: `GET /pipeline/{id}`, `PATCH /pipeline/{id}/stage/{stage}/input`, `PATCH /pipeline/{id}/stage/{stage}/output`, `POST /pipeline/{id}/retry/{stage}`, `POST /pipeline/{id}/stage/{stage}/media`
- Step-by-step: `GET /pipeline/{projectId}/steps/run-state`, `GET /pipeline/{projectId}/steps/current`, `POST /pipeline/{projectId}/steps/next`
- Projects: `GET /projects`
- Settings: `GET|PUT /settings/llm`, `GET|PUT /settings/agents`
- Webhooks: `POST /webhooks/{provider}`

## Infrastructure (docker-compose)

Services: `nats` (2.10-alpine), `postgres` (16-alpine, persistent volume `waoo-pgdata`), `redis` (7-alpine), `backend`, `web`

Published host ports: backend `8082:8080`, web `3003:3000`. Internal services không publish host port.

**Không** có MinIO service trong compose (mặc dù config.go và .env.example có S3 config).

Lưu ý port khi dev local: backend chạy trên `8080`, `web/.env.local` trỏ đến `http://localhost:8080`. Docker compose map ra `8082`.

## Configuration

- Config load hoàn toàn từ env vars (`internal/config/config.go`). Copy `.env.example` → `.env` trước khi chạy.

Key env groups:
- Server: `SERVER_HOST` (0.0.0.0), `SERVER_PORT` (8080)
- LLM: `OPENROUTER_API_KEY`, `OPENROUTER_BASE_URL`, `GOOGLE_AI_KEY`, `ANTHROPIC_KEY`
- LLM models: `LLM_FLASH_MODEL`, `LLM_STANDARD_MODEL`, `LLM_PREMIUM_MODEL`, `LLM_DEFAULT_BUDGET_USD`, `LLM_REQUEST_TIMEOUT_S`
- Media: `FAL_KEY`, `ARK_KEY`, `MINIMAX_KEY`, `VIDU_KEY`, `QWEN_KEY`
- DB: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`, `DB_MAX_CONNS`
- Redis: `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB`
- NATS: `NATS_URL` (nats://localhost:4222), `NATS_CLUSTER_ID`, `NATS_MAX_RECONNECTS`, `NATS_RECONNECT_WAIT`, `NATS_REQUEST_TIMEOUT`
- S3/MinIO: `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_REGION`, `S3_USE_SSL`
- Auth: `KEYCLOAK_URL`, `KEYCLOAK_REALM`
- Webhooks: `WEBHOOK_SECRET`
- Feature flags: `AUTH_ENABLED` (default `false`)
- Frontend: `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_KEYCLOAK_URL`, `NEXT_PUBLIC_KEYCLOAK_REALM`, `NEXT_PUBLIC_KEYCLOAK_CLIENT_ID`

Lưu ý: `.env.example` dùng port khác với code defaults cho DB (5434 vs 5432) và Redis (6381 vs 6379) — do .env.example dùng Docker port mapping.

## Remaining TODOs

- `cmd/worker` và `cmd/cli` chưa implement (commented out trong Makefile)
- `project/[id]/page.tsx` rất lớn (~1550 dòng) — nên tách components
- MinIO service chưa có trong docker-compose (config và .env.example đã có S3 settings)
- Workflow engine đã init nhưng chưa wire vào HTTP routes
- Repo chưa có `*_test.go` files

## Code Conventions

- Constructor-based dependency injection (no global service container)
- Interface-driven core contracts trong `internal/agent/interfaces.go`
- Structured logging với `slog`
- Error wrapping pattern `fmt.Errorf("...: %w", err)`
- Concurrency primitives: goroutines + mutex/waitgroup theo từng subsystem
- Go module: `github.com/uni-ai-studio/waoo-studio`, Go 1.25.7
- Direct deps: `pgx/v5`, `nats.go`, `go-redis/v9`, `golang-jwt/jwt/v5`, `google/uuid`
