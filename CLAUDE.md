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

## Commands

### Backend (Go) — chạy từ thư mục gốc

```bash
make dev          # go run ./cmd/server
make build        # Build: bin/waoo-server, bin/waoo-worker, bin/waoo-cli
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

> Lưu ý: hiện tại repo gần như chưa có `*_test.go`; `make test` vẫn chạy toàn bộ package testable.

## High-level Architecture

### Backend

- Entry point: `cmd/server/main.go`
- Core flow startup:
  1. Load env config (`config.Load()`)
  2. PostgreSQL init (graceful degradation nếu fail)
  3. NATS init (**hard requirement**, fail thì exit)
  4. LLM router + tool registry + memory store + async poller + webhook handler
  5. Pipeline engines (Autopilot + Step-by-Step)
  6. Agent registry/supervisor (register 6 agents)
  7. HTTP server (`net/http`)

Dependency behavior:
- NATS là dependency bắt buộc.
- PostgreSQL/Redis unavailable → server vẫn chạy với degraded capabilities.

### Agent System

- Core interface: `internal/agent/types.go` (`Card`, `HandleMessage`, `HandleStream`, `Name`)
- Shared base implementation: `internal/agent/base.go`
- BaseAgent LLM helpers (`CallLLM`, `CallLLMWithJSON`) đi qua per-agent routing (`CallForAgent`/`CallWithJSONForAgent`) để hỗ trợ agent model overrides.
- NATS subject convention: `agent.{name}.{skillId}`
- Queue group per agent: `agent-{name}-workers`

Agents trong `internal/agents/`:
- director
- character
- location
- storyboard
- media
- voice

### Pipeline

Stages (11):
`analysis → planning → characters → locations → segmentation → screenplay → storyboard → media_gen → quality_check → voice → assembly`

- `characters` và `locations` chạy song song trong autopilot flow.
- Hai mode:
  - **Autopilot**: Director orchestrates end-to-end.
  - **Step-by-Step**: human-gated, có checkpoint/retry/edit theo stage.

### Memory / Async / External providers

- Tiered memory: hot (in-process) + warm (Redis) + cold (PostgreSQL)
- Async task polling + persistence: `internal/poller/`
- External media/tool wrappers: `internal/tools/`
- Provider webhook normalization: `internal/webhook/`

### Frontend (Next.js)

- Stack: Next.js 16, React 19, TypeScript 5, Tailwind v4
- App Router trong `web/app/`
- API wrapper: `web/lib/api.ts`
- SSE client: `web/lib/sse.ts`
- Keycloak integration: `web/lib/keycloak.ts`, `web/app/providers.tsx`

Auth status hiện tại:
- Frontend có Keycloak login/token refresh và attach Bearer token cho API calls.
- Backend có auth middleware implementation (`internal/auth/middleware.go`), nhưng hiện server handler đang dùng `corsMiddleware(mux)` trực tiếp (middleware auth chưa được bật trong chain).

## API Surface (thường dùng)

Các endpoint chính:
- `GET /health`
- `GET /agents`
- `GET /agents/health`
- `GET /agents/{name}`
- `POST /agents/{name}/send`
- `GET /tools`
- `POST /pipeline/start`
- `GET /pipeline/progress/{projectId}` (SSE)
- `GET /projects`
- `GET /settings/llm`, `PUT /settings/llm`
- `GET /settings/agents`, `PUT /settings/agents`

Step-by-step/review endpoints cũng có trong `cmd/server/main.go` (`/pipeline/{id}/...`, `/pipeline/{projectId}/steps/...`).

## Infrastructure (docker-compose hiện tại)

Services:
- `nats`
- `postgres`
- `redis`
- `backend`
- `web`

Published host ports:
- backend: `8082:8080`
- web: `3003:3000`

Ghi chú:
- Compose hiện tại **không** định nghĩa MinIO service.
- Internal services (postgres/redis/nats) không publish host port trong compose hiện tại.

## Configuration

- Config load hoàn toàn từ env vars (`internal/config/config.go`)
- Copy `.env.example` → `.env` trước khi chạy local.

Key env names (đúng theo code hiện tại):
- LLM: `OPENROUTER_API_KEY`, `GOOGLE_AI_KEY`, `ANTHROPIC_KEY`
- Media: `FAL_KEY`, `ARK_KEY`, `MINIMAX_KEY`, `VIDU_KEY`, `QWEN_KEY`
- DB: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`
- Auth: `KEYCLOAK_URL`, `KEYCLOAK_REALM`

## Code Conventions

- Constructor-based dependency injection (no global service container)
- Interface-driven core contracts trong `internal/agent/interfaces.go`
- Structured logging với `slog`
- Error wrapping pattern `fmt.Errorf("...: %w", err)`
- Concurrency primitives: goroutines + mutex/waitgroup theo từng subsystem
