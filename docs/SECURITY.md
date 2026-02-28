# PairProxy Security Guide

This document describes the security model of PairProxy, the threats it addresses, the mitigations in place, and operational hardening recommendations.

---

## Threat Model

PairProxy sits between internal developer tooling (Claude Code) and commercial LLM APIs. The primary threats are:

| Threat | Impact |
|--------|--------|
| Unauthorized LLM access | API cost without accountability |
| Token theft / replay | Impersonation, quota bypass |
| JWT algorithm confusion | Forged tokens accepted by s-proxy |
| Unauthenticated cluster API | Worker injection, usage data manipulation |
| Config misconfiguration at startup | Silent security degradation |
| Resource exhaustion (quota bypass) | Runaway API spend |

---

## JWT Authentication (User ↔ s-proxy)

### Algorithm Pinning (HS256 only)

All JWTs issued by `s-proxy` use **HMAC-SHA256 (HS256)**. The `Parse()` function
enforces this with an explicit algorithm check:

```
if token.Method.Alg() != "HS256" → reject with ErrInvalidToken
```

This prevents **algorithm confusion attacks** where an attacker signs a token
with a different HMAC variant (HS384, HS512) or an asymmetric algorithm
(RS256 with the HS256 secret as the public key) in an attempt to bypass
signature verification.

**What is NOT accepted:**
- `HS384`, `HS512` — even if signed with the correct secret
- `RS256`, `ES256` — asymmetric algorithms
- `none` — unsigned tokens

### JWT Blacklist (Token Revocation)

A short-lived in-memory blacklist stores revoked JTI (JWT ID) values. When
`sproxy admin token revoke <username>` is called:

1. The user's refresh token is deleted from the database.
2. The access token's JTI is added to the in-memory blacklist with a TTL equal
   to the token's remaining validity.

On `Parse()`, blacklisted JTIs are rejected with `ErrTokenRevoked`.

**Caveat**: the blacklist is in-memory. A s-proxy restart clears it. On
restart, still-valid access tokens from revoked users will be accepted until
they expire naturally (≤ 24h by default). For immediate revocation, set
`access_token_ttl` to a short value (e.g., `1h`).

### Secure Secret Management

- `auth.jwt_secret` **must** be set (config validation rejects empty values).
- Use environment variable substitution (`${JWT_SECRET}`) — never commit secrets
  to version control.
- Minimum recommended length: 32 random bytes (`openssl rand -hex 32`).

---

## Cluster Internal API Authentication (Primary ↔ Worker)

The cluster internal API (`/api/internal/register`, `/api/internal/usage`,
`/cluster/routing`) uses a **shared Bearer token** (`cluster.shared_secret`).

### Fail-Closed Policy

The primary operates **fail-closed**: if `shared_secret` is empty, **all**
requests to the cluster internal API are rejected with HTTP 401 and a WARN
log entry. There is no unauthenticated mode.

| Scenario | Behavior |
|----------|----------|
| `shared_secret` empty on primary | All cluster API requests → 401 (WARN logged) |
| `shared_secret` set, correct token | Request accepted |
| `shared_secret` set, wrong token | Request → 401 (WARN logged) |
| `shared_secret` set, no Authorization header | Request → 401 (WARN logged) |

### Deployment Requirements

1. Generate a strong secret (≥ 32 random bytes):
   ```bash
   openssl rand -hex 32
   ```
2. Set it as `cluster.shared_secret` on the **primary** and all **workers**
   using the same value.
3. Use `${CLUSTER_SECRET}` substitution — inject via environment variable.
4. Restrict network access to `/api/internal/*` to the cluster's private
   network only (firewall / security group rules).

### Single-Node Deployments

On a single-node deployment (no workers), leave `shared_secret` empty. The
internal API will never be called by any worker, so the fail-closed rejection
is harmless.

---

## Config Validation at Startup

Both `LoadSProxyConfig` and `LoadCProxyConfig` run a `Validate()` check
immediately after loading and applying defaults. The process exits early with a
descriptive error if any required field is missing or out of range.

### Fields Validated (s-proxy)

| Field | Rule |
|-------|------|
| `auth.jwt_secret` | Must be non-empty |
| `database.path` | Must be non-empty |
| `llm.targets` | Must have at least one entry |
| `listen.port` | Must be in range 1–65535 |
| `cluster.role` | Must be `primary` or `worker` (or empty, treated as `primary`) |
| `cluster.primary` | Required when `role = worker` |

### Multiple Errors

`Validate()` collects **all** validation errors before returning, so a
misconfigured deployment reports every problem at once rather than failing on
the first error.

### Fields Validated (c-proxy)

| Field | Rule |
|-------|------|
| `listen.port` | Must be in range 1–65535 |

---

## Database Connection Lifecycle

Both the `sproxy start` command and all `sproxy admin` sub-commands open a GORM
database connection and close it via `defer` before the process exits:

```
sproxy start  → defer closeGormDB()  (covers both normal exit and fatal errors)
sproxy admin  → each subcommand defers closeGormDB() individually
```

This prevents SQLite WAL file corruption and leaked file descriptors.

---

## Quota and Rate Limiting

### Quota Enforcement (daily / monthly tokens)

Quotas are enforced per user group. When a user exceeds their group quota:
- The request is rejected with HTTP **429 Too Many Requests**.
- Response headers `X-RateLimit-Limit`, `X-RateLimit-Used`, `X-RateLimit-Reset`
  provide machine-readable quota information.
- An optional alert webhook is called (async, fire-and-forget).

**Fail-open caveat**: if the database is unreachable during a quota check, the
request is **allowed** (fail-open). This prioritizes availability over strict
enforcement. If strict enforcement is critical, monitor database health.

### Rate Limiting (requests per minute)

RPM limits use a per-user sliding window (1-minute window). When the limit is
exceeded, the request is rejected with HTTP **429**. The rate limiter is
automatically purged of stale entries every minute.

---

## Password Security

Admin passwords are stored as **bcrypt hashes** (work factor 12 by default).
Plaintext passwords are never written to disk or logged.

User login credentials are transmitted only over HTTPS. In development
environments, ensure that the connection between c-proxy and s-proxy uses TLS
(e.g., place s-proxy behind an nginx/Caddy reverse proxy with a TLS certificate).

---

## Logging and Auditing

PairProxy uses structured logging (zap) with the following security-relevant
log entries:

| Event | Level | Fields |
|-------|-------|--------|
| JWT verification failure | WARN | `remote_addr`, `path`, `error` |
| JWT algorithm mismatch | WARN | `got_alg`, `want_alg` |
| Cluster API auth failure | WARN | `remote_addr`, `path` |
| Cluster `shared_secret` not configured | WARN | `remote_addr`, `path` |
| Quota exceeded | WARN | `user_id`, `kind` (daily/monthly/rate_limit), `reset_at` |
| Admin login failure | WARN | implicit (401 response) |
| Token revoked | INFO | `user_id`, `jti` |

Set `log.level: debug` during security testing to enable per-request verbose logs.

---

## Reporting Vulnerabilities

Please report security vulnerabilities by opening a **private** GitHub Security
Advisory at: `https://github.com/l17728/pairproxy/security/advisories/new`

Do **not** file public GitHub issues for security vulnerabilities.
