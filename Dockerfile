FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache gcc musl-dev

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o waoo-server ./cmd/server

# Final lightweight stage
FROM alpine:latest

WORKDIR /app

# Install dependencies required for runtime (like timezone data, ca-certificates, etc.)
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/waoo-server .
COPY --from=builder /app/.env .

# Expose port (adjust if necessary)
EXPOSE 8080

CMD ["./waoo-server"]
