# Build stage
FROM golang:1.27.1-alpine AS builder

WORKDIR /build

# Copy only go.mod and go.sum for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app ./cmd/api

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/app .

# Expose port (matches APP_PORT default in cmd/api/main.go)
EXPOSE 8080

# No HEALTHCHECK here: distroless has no shell/curl, and readiness probe
# is a separate task (see docs/ROADMAP.md Minggu 5).

# Set entrypoint (distroless:nonroot already runs as nonroot user)
ENTRYPOINT ["/app/app"]
