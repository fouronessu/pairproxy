# Dockerfile for Claude Load Tester

# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN make build

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 1000 loadtest

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/build/claude-load-tester /app/claude-load-tester

# Copy default prompts and config
COPY --from=builder /build/prompts /app/prompts
COPY --from=builder /build/config /app/config

# Create results directory
RUN mkdir -p /app/results && chown -R loadtest:loadtest /app

# Switch to non-root user
USER loadtest

# Expose API port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/status || exit 1

# Default entrypoint
ENTRYPOINT ["/app/claude-load-tester"]

# Default command
CMD ["run", "--api-enabled", "--api-addr", ":8080"]
