# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WAOO Studio** – AI Filmmaking Platform. Backend bằng Go, frontend bằng Next.js. Hệ thống dựa trên kiến trúc multi-agent để tự động hóa quy trình sản xuất phim (screenplay → storyboard → image/video/voice generation).

## Commands

### Backend (Go) – chạy từ thư mục `waoo-studio/`

```bash
make dev          # Chạy server (go run ./cmd/server)
make build        # Build binaries → bin/waoo-server, bin/waoo-worker, bin/waoo-cli
make test         # go test -race -cover ./...
make lint         # golangci-lint run ./...
make deps         # go mod tidy
make generate     # go generate ./...

# Infrastructure (Docker Compose)
make infra        # Start PostgreSQL, NATS, Redis, MinIO
make infra-down   # Stop infrastructure
make infra-reset  # Destroy volumes + restart

# Database
make migrate      # Apply all SQL files in migrations/
```

### Frontend – chạy từ thư mục `waoo-studio/web/`

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

Entry point: `cmd/server/main.go` khởi tạo toàn bộ hệ thống theo thứ tự:
1. Load config từ env vars (`internal/config/`)
2. Connect PostgreSQL (`pgx/v5`) + NATS (`nats.go`)
3. Khởi tạo LLM Router, Tool Registry, Tiered Memory, Async Poller, Webhook Handler
4. Đăng ký 6 agents vào Supervisor
5. Start HTTP server (stdlib `net/http`, không dùng framework)

**Cấu trúc `internal/`:**

| Package | Chức năng |
|---|---|
| `agent/` | Interface, base, registry, supervisor cho tất cả agents |
| `agents/{name}/` | Implementations: director, character, location, storyboard, media, voice |
| `config/` | Đọc env vars, không có file config |
| `llm/` | LLM Router với 3 model tier + circuit breaker |
| `memory/` | Tiered memory: hot (Redis) + cold (PostgreSQL) |
| `natsbus/` | NATS message bus wrapper |
| `pipeline/` | Filmmaking pipeline orchestration + checkpointing |
| `poller/` | Async task polling với PostgreSQL persistence |
| `qualitygate/` | Output evaluation + retry logic |
| `tools/` | Tool Registry cho agents |
| `webhook/` | Webhook handler cho external providers |
| `workflow/` | Workflow engine + events |
| `worldstate/` | Shared world state (characters, locations, scenes) |

**Prompts:** `lib/prompts/` – file `.txt` theo format `{task}.{lang}.txt` (en/zh). Load qua `lib/prompts/loader.go`.

**LLM Model Tiers** (quan trọng khi gọi LLM):
- `LLM_FLASH_MODEL` – tác vụ đơn giản, nhanh (default: `google/gemini-2.0-flash-exp`)
- `LLM_STANDARD_MODEL` – tác vụ phức tạp (default: `anthropic/claude-sonnet-4-*`)
- `LLM_PREMIUM_MODEL` – tác vụ quan trọng nhất (default: `anthropic/claude-opus-4-*`)

### Frontend Next.js

Stack: Next.js 16 + React 19 + TypeScript + Tailwind CSS, App Router.

Pages trong `web/app/`:
- `/` – Dashboard/home
- `/new` – Tạo project mới
- `/project/[id]` – Project detail view
- `/agents` – Agent management
- `/tools` – Tool listing
- `/settings` – LLM settings

Frontend giao tiếp với backend Go API (`localhost:8082` theo `.env`).

### API Endpoints (Backend)

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

### Infrastructure (Docker Compose)

| Service | Port | Dùng cho |
|---|---|---|
| PostgreSQL 16 | `5434:5432` | Primary DB + cold memory + task persistence |
| NATS 2.10 | `4222`, `8222` | Agent messaging bus |
| Redis 7 | `6379` | Hot memory cache |
| MinIO | `9000`, `9001` | S3-compatible object storage (media files) |

### Cấu hình

Tất cả config đọc từ env vars. Copy `.env.example` → `.env` để bắt đầu. Các key quan trọng:
- `OPENROUTER_API_KEY` / `GOOGLE_AI_KEY` / `ANTHROPIC_KEY` – LLM providers
- `DB_PORT=5434` (không phải 5432 mặc định – PostgreSQL chạy trên cổng custom)
- `SERVER_PORT=8082`

## Development Workflow

1. Copy `.env.example` → `.env` và điền API keys
2. `make infra` – khởi động infrastructure
3. `make migrate` – apply database migrations
4. `make dev` – chạy backend Go
5. `cd web && npm run dev` – chạy frontend

Sau khi thay đổi Go code trong Docker: rebuild binary và restart container.
