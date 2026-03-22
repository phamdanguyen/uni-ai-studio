# ========== Builder ==========
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/waoo-server ./cmd/server

# ========== Runtime ==========
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget tzdata

WORKDIR /app

# Copy binary
COPY --from=builder /app/bin/waoo-server ./bin/waoo-server

# Copy prompts — runtime.Caller(0) resolves to /app/lib/prompts/loader.go at compile time
# so prompt files must exist at /app/lib/prompts/ in runtime container
COPY --from=builder /app/lib/prompts/ ./lib/prompts/

# Copy migrations for DB init
COPY --from=builder /app/migrations/ ./migrations/

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./bin/waoo-server"]
