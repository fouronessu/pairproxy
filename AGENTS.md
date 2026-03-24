# PairProxy Agent Guide

This Go-based LLM proxy service provides enterprise-grade rate limiting, token tracking, and multi-tenant management for Claude Code and other LLM clients.

## Build & Test Commands

### Build
```bash
make build          # Build cproxy and sproxy to bin/
make build-dev      # Build all binaries (including mockllm/mockagent) to release/
make release        # Cross-platform release packages to dist/
```

### Test
```bash
make test           # Run all tests
make test-race      # Run with race detector (slower)
make test-cover     # Generate coverage.html report
make test-pkg PKG=./internal/quota/...  # Run single package tests
```

### Code Quality
```bash
make fmt            # Format with gofmt/goimports
make vet            # Run go vet
make lint           # Run golangci-lint (install first: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
make tidy           # Update go.mod/go.sum
```

### Development
```bash
make run-sproxy     # Run sproxy with example config (requires config/sproxy.yaml)
make run-cproxy     # Run cproxy with example config
make bcrypt-hash    # Generate admin password hash for sproxy.yaml
```

## Code Style Guidelines

### Basics
- **Go version**: 1.24.0 (per go.mod)
- **Package names**: Lowercase, no underscores, short and descriptive
- **Imports**: Grouped by standard library, then external, then internal
- **Line length**: No hard limit, but keep readable (< 120 chars preferred)
- **Comments**: Use English or Chinese based on existing file style

### Naming Conventions
- **Types**: PascalCase (`LLMTarget`, `Checker`, `ExceededError`)
- **Functions/Variables**: camelCase (`checkQuota`, `llmBalancer`, `apiKeyResolver`)
- **Test functions**: `TestXxx` pattern, descriptive names (`TestCheckerNoGroup`)
- **Error variables**: `ErrXxx` (`ErrNoLLMBinding`, `ErrBoundTargetUnavailable`)

### Formatting
- **Auto-format**: `make fmt` before commit
- **Imports**: `goimports` manages import order
- **No trailing spaces**, Unix line endings

### Error Handling
- **Wrap with context**: `fmt.Errorf("context: %w", err)`
- **Never ignore errors**: Add `//nolint:errcheck` with comment if intentional
- **Public APIs**: Must handle all error paths
- **Fail-open**: Quota/database errors should not block requests (log and bypass)

### Logging (Zap)
- **Package logger**: Create with `logger.Named("subsystem")`
- **Levels**:
  - `DEBUG`: Per-request details (token counts, SSE parsing), disabled in prod
  - `INFO`: Lifecycle events (start, shutdown, token reload)
  - `WARN`: Recoverable errors (DB write failure, health check failure)
  - `ERROR`: Non-recoverable errors requiring manual intervention
- **Always add context**: `zap.String("user_id", id), zap.Error(err)`

### Testing
- **Test files**: `xxx_test.go` in same package or `_test` pkg for black-box
- **No external assert frameworks**: Use standard `testing` package only
- **Test helpers**: Pattern `setupXxxTest(t *testing.T)` returning cleanups
- **No network/FS side effects**: Use `httptest.NewServer`, temp directories
- **Race detection**: `make test-race` before merging

### Configuration
- **YAML format**: Snake_case keys, use `${ENV_VAR}` for secrets
- **Example configs**: `config/*.yaml.example` serve as reference
- **Load config**: `config.Load(path)` from `internal/config`
- **Environment variables**: Required for secrets (JWT_SECRET, API keys)

### API Conventions
- **REST endpoints**: `/api/admin/*`, `/api/internal/*`, `/api/user/*`
- **JWT auth**: `X-PairProxy-Auth` or `Authorization: Bearer <token>`
- **Pagination**: Query params `page` (default 1) and `page_size` (default 100)
- **Error responses**: JSON with `error` and `message` fields

### Database (GORM)
- **Driver**: SQLite (default) or PostgreSQL (`driver: "postgres"`)
- **Migration**: `db.Migrate(logger, gormDB)` on startup
- **Async writes**: `UsageWriter` with buffer + flush interval
- **PostgreSQL**: Use `gorm.io/driver/postgres`, all nodes share DB (Peer Mode)

### Observability
- **OpenTelemetry**: Tracing via `go.opentelemetry.io/otel`
- **Prometheus metrics**: `GET /metrics` endpoint (cache-refreshed every 30s)
- **Health check**: `GET /health` returns status + uptime + DB connectivity

## CI/CD

### CI Workflow (`.github/workflows/ci.yml`)
- **Matrix**: Go 1.24
- **Checks**: `go build`, `go vet`, `go test -race -count=1`, lint
- **Coverage**: Upload artifact for report aggregation

### Release Workflow (`.github/workflows/release.yml`)
- **Tags**: Push `git tag v1.2.3 && git push origin v1.2.3`
- **Auto-builds**: Cross-compile 5 platforms (Linux/macOS/Windows × amd64/arm64)
- **Docker**: Multi-arch image `ghcr.io/l17728/pairproxy`

## Architecture Summary

| Layer | Folder | Purpose |
|-------|--------|---------|
| CLI entrypoints | `cmd/cproxy/`, `cmd/sproxy/` | Cobra commands for user-facing binaries |
| Core proxy logic | `internal/proxy/` | HTTP handlers, middleware, protocol conversion |
| Auth & JWT | `internal/auth/` | Token issuance/refresh, bcrypt password hashing |
| Quota management | `internal/quota/` | Per-user/group daily/monthly/configured limits, rate limiting |
| Database | `internal/db/` | GORM models, repositories (User, Group, Usage, LLMTarget) |
| Load balancing | `internal/lb/` | `WeightedRandomBalancer`, health check |
| Config | `internal/config/` | YAML loader, validation, env var expansion |
| Dashboard | `internal/dashboard/` | Web UI (Go templates + Tailwind, embedded in binary) |
| Tracking | `internal/track/` | Full conversation recording per user |
|_corpus | `internal/corpus/` | Training data collection (JSONL, v2.16.0+) |
| Semantic router | `internal/router/` | Intent-based LLM target selection (v2.18.0+) |

## Special Considerations

### Unicode/Internationalization
- Project uses Chinese comments in many files — match existing file style
- No formal i18n framework; English for logs/API, Chinese for internal docs

### Version Injection
- Built binaries embed version via ldflags: `Version`, `Commit`, `BuiltAt` from `internal/version`

### Protocol Support
- **Anthropic**: `/v1/messages` (default provider)
- **OpenAI**: `/v1/chat/completions` (protocol auto-detection via `provider: openai`)
- **Ollama**: `/v1/chat/completions` (OpenAI-compatible, `provider: ollama`)
- **Auto-conversion**: Interoperability between Anthropic/OpenAI formats (Claude CLI → Ollama)

### Cluster Modes
- **SQLite**: Primary + Workers (30s sync window, primary only for writes)
- **PostgreSQL**: Peer Mode (v2.14.0+, all nodes equal, shared DB)

### Direct Proxy (v2.9.0+)
- `sk-pp-` API Key format for headerless access
- HMAC-SHA256 based (`auth.keygen_secret` required), 48-char Base62 encoded

## Projectile-Specific Patterns

### Common Patterns to Follow

**Error Propagation**:
```go
func (s *SProxy) handleRequest(ctx context.Context, req *http.Request) error {
    user, err := s.userRepo.GetByID(userID)
    if err != nil {
        s.logger.Warn("failed to get user, bypassing", zap.Error(err))
        return nil // fail-open
    }
    // ...
}
```

**Test Helper**:
```go
func setupTestDB(t *testing.T) *gorm.DB {
    t.Helper()
    logger := zaptest.NewLogger(t)
    db, err := db.Open(logger, ":memory:")
    if err != nil {
        t.Fatalf("db.Open: %v", err)
    }
    if err := db.Migrate(logger, db); err != nil {
        t.Fatalf("db.Migrate: %v", err)
    }
    return db
}
```

**Health Check Pattern**:
```go
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if err := h.checkDB(); err != nil {
        http.Error(w, "database unavailable", http.StatusServiceUnavailable)
        return
    }
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

## PR Checklist

- [ ] `make fmt` and `make vet` pass
- [ ] `make test` passes (or `make test-race` for concurrent changes)
- [ ] New features have tests (unit or integration)
- [ ] Configuration changes reflected in `config/*.yaml.example`
- [ ] CLI commands documented in `CLAUDE.md`
- [ ] Public APIs have godoc comments
- [ ] Breaking changes tracked in `docs/UPGRADE.md`
