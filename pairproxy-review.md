# PairProxy Review — Summary of Recommendations

**Project:** [l17728/pairproxy](https://github.com/l17728/pairproxy)  
**Review Date:** 2026-03-09  
**Reviewer:** OpenClaw Assistant 🦎

---

## Executive Summary

PairProxy is **enterprise-grade software** with a solid security model, clean architecture, and production-ready deployment options. The cproxy/sproxy split is well-reasoned, and the cluster design handles real-world failure scenarios.

**Verdict:** Production-ready for teams of 50-200 developers. Larger deployments should address scalability concerns below.

---

## 🔴 Critical Concerns

### 1. Conversation Tracking — Privacy & Compliance Risk

**Issue:** Full conversation content stored as plaintext JSON files with `0644` permissions.

**Risk:** GDPR/PIPL violations, data exposure if server is compromised.

**Recommendations:**
- Default to `0600` file permissions
- Add optional encryption-at-rest (AES-GCM, key from env var)
- Add automatic retention deletion (`--retention-days 30`)
- **Make opt-in explicit** with warning about legal/regulatory requirements
- Document in SECURITY.md that this feature requires legal review before production use

**Priority:** HIGH — Enable only with explicit user consent and legal approval.

---

### 2. SQLite Scalability Ceiling

**Issue:** Single SQLite database for usage_logs will hit write contention at scale.

**Threshold:** ~50-100 developers = fine. 200+ = consider alternatives.

**Recommendations:**
- Add PostgreSQL backend as optional alternative
- Keep SQLite as default for single-node deployments
- Abstract the `UsageRepo` interface to support multiple backends
- Document performance characteristics in `docs/PERFORMANCE.md`

**Priority:** MEDIUM — Not urgent now, but limits growth.

---

### 3. JWT Blacklist is In-Memory

**Issue:** Revoked tokens become valid again after sproxy restart (until natural expiration).

**Risk:** Revoked users regain access for up to 24h (default access token TTL).

**Recommendations:**
- Persist blacklist to SQLite with TTL-based cleanup
- Or use Redis for shared revocation across cluster nodes
- Alternatively: reduce `access_token_ttl` to 1h for high-security deployments

**Priority:** MEDIUM — Documented limitation, but should be fixed.

---

## 🟡 Important Improvements

### 4. Quota Enforcement is Fail-Open

**Issue:** If database is unreachable, requests are allowed (prioritizes availability).

**Risk:** Runaway API costs during DB outages.

**Recommendation:**
- Add config flag: `quota.fail_closed: true`
- When enabled, return 503 on DB errors instead of allowing requests
- Document trade-off: availability vs. cost control

**Priority:** MEDIUM — Depends on team's risk tolerance.

---

### 5. Single Primary = Single Point of Failure

**Issue:** Primary node is the routing table source + usage aggregator. If it dies:
- Workers continue serving traffic (good)
- Usage data fragments until primary recovers (bad)
- No automatic primary election

**Recommendations:**
- Document this limitation clearly in `docs/CLUSTER_DESIGN.md`
- Recommend monitoring + alerting on primary health
- Consider Raft-based consensus for primary election (future enhancement)
- Or: document multi-primary as "not supported" and recommend fast restart procedures

**Priority:** MEDIUM — Acceptable for most teams, but should be documented.

---

### 6. No Test Coverage Visibility

**Issue:** README shows `make test-cover` but no actual coverage numbers or badge.

**Recommendation:**
- Add coverage badge to README
- Aim for 70%+ on core packages: `auth`, `proxy`, `quota`, `cluster`
- Publish coverage report in CI artifacts

**Priority:** LOW — Doesn't affect functionality, but builds trust.

---

## 🟢 Quick Wins

### 7. Add cproxy Health Check

**Current:** Only sproxy has `/health` endpoint.

**Recommendation:**
```bash
cproxy status --verbose
# Output: JWT validity, sproxy connectivity, routing table version
```

**Priority:** LOW — Nice to have for debugging.

---

### 8. Rate Limit Admin API

**Current:** Login brute-force protection exists (5 failures → 5min lockout).

**Gap:** Admin endpoints (user creation, quota changes, token revocation) are not rate-limited.

**Recommendation:**
- Add per-IP rate limiting on `/api/admin/*` endpoints
- Suggest: 60 requests/minute for authenticated admin sessions

**Priority:** LOW — Security hardening.

---

### 9. Add Request Tracing IDs

**Current:** No correlation ID propagated through the request chain.

**Recommendation:**
- Generate `X-Request-ID` at cproxy
- Propagate: cproxy → sproxy → LLM
- Log in all three places for debugging

**Priority:** LOW — Improves observability.

---

### 10. Document Gotchas

**Create:** `docs/GOTCHAS.md` with operational landmines:

```markdown
# Operational Gotchas

## JWT Secret Rotation
Rotating the JWT secret invalidates ALL existing tokens immediately.
Plan for a maintenance window. Users must re-run `cproxy login`.

## SQLite WAL Files
On crash, WAL files may need manual cleanup:
  rm /var/lib/pairproxy/pairproxy.db-wal
  rm /var/lib/pairproxy/pairproxy.db-shm

## SSE Streaming
Requires proxy buffering disabled. nginx: `proxy_buffering off;`
Caddy: `flush_interval -1`
```

**Priority:** LOW — Helps operators avoid mistakes.

---

## ✅ What's Already Excellent

Don't change these — they're done right:

| Area | Why It's Good |
|------|---------------|
| **JWT Algorithm Pinning** | HS256-only, prevents algorithm confusion attacks |
| **Fail-Closed Cluster API** | No accidental unauthenticated mode |
| **Config Validation** | Catches misconfigurations at startup |
| **Secret Management** | Env var substitution, no hardcoded secrets |
| **Docker Images** | Distroless, ~15MB, non-root, no shell |
| **systemd Hardening** | NoNewPrivileges, ProtectSystem, PrivateTmp, etc. |
| **TLS Guidance** | nginx + Caddy examples with modern TLS config |
| **Developer UX** | Zero-invasion setup, auto-refresh tokens, self-service dashboard |
| **Cluster Failover** | c-proxy 3-source merge (config + cache + primary) |
| **Heartbeat Design** | TTL eviction, versioned routing tables |

---

## 📋 Action Plan

### Immediate (Before Next Release)
- [ ] Fix conversation tracking file permissions (0644 → 0600)
- [ ] Add explicit opt-in warning for conversation tracking
- [ ] Document JWT blacklist limitation in SECURITY.md

### Short-Term (Next 1-2 Months)
- [ ] Persist JWT blacklist to SQLite or Redis
- [ ] Add `quota.fail_closed` config option
- [ ] Create `docs/GOTCHAS.md`
- [ ] Add test coverage badge

### Medium-Term (Next 3-6 Months)
- [ ] Add PostgreSQL backend option
- [ ] Add request tracing IDs
- [ ] Rate limit admin API
- [ ] Consider primary election mechanism (or document single-primary limitation)

---

## Final Thoughts

This is **impressive work**. The architecture is clean, the security model is thoughtful, and the documentation is thorough. The concerns above are mostly about scaling beyond the current design envelope or hardening edge cases.

**For teams evaluating this:** It's a strong choice. The limitations only matter at larger scales (200+ developers) or in high-security environments.

**For the maintainer:** Ship it, get users, and iterate. The foundation is solid.

---

*Generated by OpenClaw Assistant* 🦎
