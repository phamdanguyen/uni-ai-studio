# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WAOO Studio** – AI Filmmaking Platform. Backend bằng Go, frontend bằng Next.js. Hệ thống dựa trên kiến trúc multi-agent (Google A2A protocol) để tự động hóa quy trình sản xuất phim: story text → analysis → storyboard → image/video/voice generation → assembly.

## Commands

### Backend (Go) – chạy từ thư mục gốc

```bash
make dev          # go run ./cmd/server
make build        # Build → bin/waoo-server, bin/waoo-worker, bin/waoo-cli
make test         # go test -race -cover ./...
make lint         # golangci-lint run ./...
make deps         # go mod tidy
make generate     # go generate ./...

# Infrastructure (Docker Compose)
make infra        # Start PostgreSQL, NATS, Redis, MinIO
make infra-down   # Stop infrastructure
make infra-reset  # Destroy volumes + restart

# Database
make migrate      # Apply all SQL files in migrations/ (uses psql)
```

### Frontend – chạy từ thư mục `web/`

```bash
npm run dev       # Next.js dev server
npm run build     # Production build
npm run lint      # ESLint
```

### Chạy một test cụ thể

```bash
go test -run TestFunctionName ./internal/path/to/package/...
```

## Architecture

### Backend Go

Module: `github.com/uni-ai-studio/waoo-studio` (Go 1.25+)

Entry point: `cmd/server/main.go` khởi tạo theo thứ tự nghiêm ngặt:
1. Config từ env vars (`config.Load()`)
2. PostgreSQL (`pgxpool`) – **graceful degradation** nếu không khả dụng
3. World State initialization
4. NATS (`natsbus.New`) – **hard requirement**, exit nếu fail
5. LLM Router, Tool Registry, Tiered Memory, Async Poller, Webhook Handler
6. Pipeline (Autopilot + Step-by-Step modes)
7. Supervisor + Agent Registry (đăng ký 6 agents, subscribe NATS)
8. HTTP Server (stdlib `net/http`, không dùng framework)

**Quy tắc dependency:** NATS là dependency duy nhất bắt buộc. PostgreSQL và Redis fail chỉ là warning.

### Core Patterns

**Agent System (A2A Protocol):**
- Interface `Agent` trong `agent/types.go`: `Card()`, `HandleMessage()`, `HandleStream()`, `Name()`
- Mọi agent embed `BaseAgent` cung cấp: `CallLLM()`, `CallLLMWithJSON()`, `AskAgent()`, `NotifyAgent()`, `UseTool()`
- **Quan trọng:** Agents phải dùng `CallForAgent`/`CallWithJSONForAgent` (không dùng bare `Call`) để per-agent model overrides hoạt động
- NATS subject convention: `agent.{name}.{skillId}`
- Queue groups: `agent-{name}-workers` (load balancing across instances)
- Constructor pattern: `New(bus, router, toolRegistry, logger)`

**Tiered Memory (3 tầng):**
- Hot: in-process `sync.Map` (max 10K entries, LRU eviction)
- Warm: Redis (30min TTL, prefix `waoo:mem:`)
- Cold: PostgreSQL (`agent_memory` table, tag-based query)
- Read-through promotion: hot → warm → cold, hit ở tầng thấp tự promote lên trên
- Functional options: `WithTier()`, `WithTTL()`, `WithTags()`

**LLM Router:**
- 3 tiers: Flash (Gemini, temp=0.3), Standard (Claude Sonnet, temp=0.7), Premium (Claude Opus, temp=0.5)
- Tất cả calls qua OpenRouter (`openrouter.ai/api/v1`)
- Circuit breaker: 5 failures → open, 30s recovery, 2 successes → recover
- Token bucket rate limiter: 4 req/s, burst 8
- Per-project USD budget tracking (default $10)
- Per-agent model overrides via `agentModels` map
- Runtime config qua REST API (`GET/PUT /settings/llm`)
- Gemma model workaround: merge system prompt vào user message

**Pipeline (11 stages):**
```
analysis → planning → characters ‖ locations → segmentation → screenplay → storyboard → media_gen → quality_check → voice → assembly
```
Characters và locations chạy **song song**.

Hai mode:
- **Autopilot**: gửi 1 message `start_production` tới Director → Director tự orchestrate qua A2A
- **Step-by-Step**: chạy từng stage, pause sau mỗi stage cho human approval/edit. State persist vào PostgreSQL giữa các step

**Checkpointing:** `pipeline_stages` (per-stage I/O), `pipeline_runs` (run state + mode)

### 6 Agents

| Agent | Skills chính | LLM Tier | Tools | Gọi agents khác |
|---|---|---|---|---|
| **director** | analyze_story, plan_pipeline, segment_clips, convert_screenplay, start_production | Standard (Flash cho plan) | Không | character, location, storyboard, media, voice |
| **character** | analyze_characters, design_visual, create/modify/regenerate | Standard (Flash cho query) | Không | Không |
| **location** | analyze_locations, create/modify/regenerate | Standard | Không | Không |
| **storyboard** | create_storyboard (4-phase), refine_panel, shot_variants | Standard (Flash cho query) | Không | character (query_appearances) |
| **media** | generate_image, generate_video, generate_batch, quality_review | Standard (Flash cho quality) | image_generator, video_generator | Không |
| **voice** | analyze_voices, design_voice, generate_tts, lip_sync | Standard | voice_designer, tts_generator, lip_sync | Không |

Director là sole orchestrator. Chỉ media và voice dùng external tools. Storyboard callback tới character cho visual references.

**TierPremium hiện không được agent nào sử dụng.**

### Other Key Subsystems

| Package | Pattern | Chi tiết |
|---|---|---|
| `worldstate/` | Event sourcing + optimistic locking | JSONB merge, `pg_notify` cho real-time changes, agent decision audit trail |
| `workflow/` | DAG execution (Temporal-inspired) | Step dependencies, quadratic backoff retry, checkpointing, event streaming |
| `qualitygate/` | LLM-based evaluation | 4 dimensions (composition, consistency, technical, narrative), threshold 0.7, max 2 retries with prompt refinement |
| `poller/` | Async task polling | 4 providers (FAL, ARK, MiniMax, Vidu), 5s interval, max 60 attempts, crash recovery from DB |
| `tools/` | External AI provider wrappers | All generators async, external ID format: `{PROVIDER}:{MEDIA_TYPE}:{endpoint}:{requestId}` |
| `webhook/` | Provider callback handler | FAL, Vidu, MiniMax, Ark normalization → `CompletionEvent` |

### Blackboard Pattern (Inter-Agent Collaboration)

`agent/collaboration.go` – concurrent-safe shared workspace:
- Named sections with ownership, versioning, change log
- Conflict resolution: `last_write_wins` (default), `owner_priority`, `merge`
- `OnChange` listeners cho reactive updates

### Frontend Next.js

Stack: Next.js 16 + React 19 + TypeScript + Tailwind CSS v4, App Router.

**Rendering:** Root layout là server component duy nhất. Tất cả pages đều `"use client"` – entirely client-rendered. Không có SSR data fetching.

**Styling:** Dark-only theme. Custom CSS classes trong `globals.css` (`.glass-card`, `.btn-primary`, `.input-field`, etc.) + inline `style={{}}` objects. Tailwind v4 (CSS-first config, không có `tailwind.config.*`).

**API layer:** `web/lib/api.ts` – `fetchJSON<T>()` wrapper, namespace-style `api.agents.*`, `api.pipeline.*`, `api.settings.*`. Base URL từ `NEXT_PUBLIC_API_URL` (default `http://localhost:8082`).

**SSE:** `web/lib/sse.ts` – `connectPipelineSSE()` cho real-time pipeline monitoring.

**Fonts:** Plus Jakarta Sans (body) + JetBrains Mono (code/data), loaded via Google Fonts `<link>`.

**Không có:** shared components directory (co-located in page files), i18n framework, auth, testing setup, UI component library.

### API Endpoints

| Method | Path | Mô tả |
|---|---|---|
| GET | `/health` | Health check |
| GET | `/agents` | List all agents |
| GET | `/agents/{name}` | Agent detail |
| POST | `/agents/{name}/send` | Send message to agent |
| GET | `/tools` | List registered tools |
| POST | `/pipeline/start` | Start filmmaking pipeline |
| GET | `/pipeline/progress/{projectId}` | SSE stream cho pipeline progress |
| POST | `/webhooks/{provider}` | Webhook receiver |
| GET/PUT | `/settings/llm` | LLM runtime config |

HTTP dùng Go 1.22+ path parameter syntax (`r.PathValue("name")`). CORS allow all origins. SSE với 30s keepalive, `WriteTimeout: 0`.

### Infrastructure (Docker Compose)

| Service | Port | Dùng cho |
|---|---|---|
| PostgreSQL 16 | `5434:5432` | Primary DB + cold memory + task persistence |
| NATS 2.10 (JetStream) | `4222`, `8222` | Agent messaging bus |
| Redis 7 | `6379` | Warm memory cache |
| MinIO | `9000`, `9001` | S3-compatible object storage |

Migrations auto-applied via `docker-entrypoint-initdb.d` mount.

### Prompt System

`lib/prompts/` – file-based templates. Pattern: `{category}/{name}.{lang}.txt` (en/zh).
- `novel-promotion/` – 26 prompt pairs (52 files) cho pipeline chính
- `character-reference/` – 2 prompt pairs (4 files) cho image-to-text
- Template variables: `{variable_name}` replaced at render time
- In-memory caching via `sync.Map`. `MustLoad()` panics on missing.
- Prefix `agent_` = system prompt cho agent LLM calls

### Database Tables (5 migrations)

Core: `world_states`, `world_events`, `agent_decisions`, `projects`
Workflow: `workflow_runs`, `workflow_steps`, `workflow_checkpoints`, `workflow_events`
Pipeline: `pipeline_stages` (with `input` JSONB), `pipeline_runs`, `pipeline_checkpoints`
Other: `agent_memory`, `async_tasks`
View: `project_pipeline_summary`

### Cấu hình

Tất cả config từ env vars – không có file config. Copy `.env.example` → `.env`.

Key quan trọng:
- `OPENROUTER_API_KEY` – LLM provider chính (bắt buộc)
- `GOOGLE_AI_KEY` / `ANTHROPIC_KEY` – optional providers
- `FAL_API_KEY`, `ARK_API_KEY`, `MINIMAX_API_KEY`, `VIDU_API_KEY` – media generation
- `DB_PORT=5434` (custom port, không phải 5432 mặc định) – nhưng `.env.example` dùng `5432`, kiểm tra `docker-compose.yml` để xác nhận
- `SERVER_PORT=8082`

## Code Conventions

- **Dependency injection** qua constructor params, không dùng globals
- **Interface-based design** cho tất cả core components (`agent/interfaces.go`)
- **Structured logging** với `slog`, mỗi component có field `"component"`
- **Error wrapping**: `fmt.Errorf("context: %w", err)` throughout
- **Concurrency**: `sync.RWMutex` trên shared state, `sync.WaitGroup` cho parallel execution
- **Code language**: Go/English. Comments mix English/Vietnamese
- **UI language**: Headings English, descriptions Vietnamese

## Development Workflow

1. Copy `.env.example` → `.env` và điền API keys
2. `make infra` – khởi động infrastructure
3. `make migrate` – apply database migrations
4. `make dev` – chạy backend Go
5. `cd web && npm run dev` – chạy frontend
