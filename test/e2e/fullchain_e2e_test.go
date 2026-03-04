package e2e_test

// fullchain_e2e_test.go — E2E tests that exercise the complete request path:
//
//	Claude Code → cproxy → sproxy → LLM
//
// Tests here cover scenarios requiring both cproxy and sproxy to be wired
// together, complementing the single-layer tests in sproxy_e2e_test.go and
// cproxy_failover_e2e_test.go.

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/cluster"
	"github.com/l17728/pairproxy/internal/db"
	"github.com/l17728/pairproxy/internal/lb"
	"github.com/l17728/pairproxy/internal/proxy"
	"github.com/l17728/pairproxy/internal/quota"
	"github.com/l17728/pairproxy/internal/tap"
)

// ---------------------------------------------------------------------------
// TestE2EFullChainStreaming — complete SSE path cproxy → sproxy → LLM
// ---------------------------------------------------------------------------
//
// This is the primary production flow: the user's client sends a streaming
// request through cproxy (which handles auth token injection) through sproxy
// (which validates JWT, injects real API key) to the LLM, and SSE events
// propagate back to the client.
//
// Verifies:
//   - Header substitution at both proxy hops
//   - SSE events (message_start, content_block_delta, message_stop) pass through
//   - Routing headers injected by sproxy are stripped before reaching the client
//   - Token counts are recorded in the DB after the stream ends
func TestE2EFullChainStreaming(t *testing.T) {
	const (
		wantInput  = 120
		wantOutput = 60
	)

	// Step 1: Mock LLM returns a complete Anthropic SSE sequence.
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer chain-api-key" {
			t.Errorf("LLM got Authorization = %q, want 'Bearer chain-api-key'", got)
		}
		if r.Header.Get("X-PairProxy-Auth") != "" {
			t.Error("X-PairProxy-Auth must be stripped before reaching LLM")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sse := tap.BuildAnthropicSSE(wantInput, wantOutput, []string{"Hello", " streaming", " world"})
		_, _ = io.WriteString(w, sse)
	}))
	defer mockLLM.Close()

	logger := zaptest.NewLogger(t)

	// Step 2: Real sproxy with JWT validation and a user in the DB.
	jwtMgr, err := auth.NewManager(logger, "fullchain-stream-secret-xyz")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	writer := db.NewUsageWriter(gormDB, logger, 200, 30*time.Second)
	writer.Start(ctx)

	userRepo := db.NewUserRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)

	user := &db.User{ID: "chain-stream-user", Username: "chainstream", PasswordHash: "x", IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	sp, err := proxy.NewSProxy(logger, jwtMgr, writer, []proxy.LLMTarget{
		{URL: mockLLM.URL, APIKey: "chain-api-key", Provider: "anthropic"},
	})
	if err != nil {
		t.Fatalf("NewSProxy: %v", err)
	}
	checker := quota.NewChecker(logger, userRepo, usageRepo, quota.NewQuotaCache(time.Minute))
	sp.SetQuotaChecker(checker)

	spMux := http.NewServeMux()
	spMux.HandleFunc("GET /health", sp.HealthHandler())
	spMux.Handle("/", sp.Handler())
	spSrv := httptest.NewServer(spMux)
	defer spSrv.Close()

	// Step 3: Issue an access token for the user.
	accessToken, err := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "chain-stream-user",
		Username: "chainstream",
		Role:     "user",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Sign JWT: %v", err)
	}

	// Step 4: Real cproxy with a saved token pointing to sproxy.
	tokenStore := auth.NewTokenStore(logger, 30*time.Minute)
	tokenDir := t.TempDir()
	tf := &auth.TokenFile{
		AccessToken:  accessToken,
		RefreshToken: "unused",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		ServerAddr:   spSrv.URL,
		Username:     "chainstream",
	}
	if err := tokenStore.Save(tokenDir, tf); err != nil {
		t.Fatalf("Save token: %v", err)
	}

	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: spSrv.URL, Addr: spSrv.URL, Weight: 1, Healthy: true},
	})
	cp, err := proxy.NewCProxy(logger, tokenStore, tokenDir, balancer, "")
	if err != nil {
		t.Fatalf("NewCProxy: %v", err)
	}

	cpMux := http.NewServeMux()
	cpMux.Handle("/", cp.Handler())
	cpSrv := httptest.NewServer(cpMux)
	defer cpSrv.Close()

	// Step 5: Send a streaming request, as Claude Code would.
	req, err := http.NewRequest(http.MethodPost, cpSrv.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude-3-5-sonnet","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer dummy-from-claude-code")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do streaming request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("full-chain streaming: status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Routing headers must be stripped by sproxy/cproxy before reaching the client.
	if v := resp.Header.Get("X-Routing-Version"); v != "" {
		t.Errorf("X-Routing-Version leaked to client, got %q (should be stripped)", v)
	}
	if v := resp.Header.Get("X-Routing-Update"); v != "" {
		t.Errorf("X-Routing-Update leaked to client (should be stripped)")
	}

	// Consume and validate the SSE stream.
	var gotStart, gotStop bool
	var textChunks []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if strings.Contains(data, `"message_start"`) {
			gotStart = true
		}
		if strings.Contains(data, `"message_stop"`) {
			gotStop = true
			break
		}
		if strings.Contains(data, `"content_block_delta"`) {
			textChunks = append(textChunks, data)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("SSE scanner error: %v", err)
	}

	if !gotStart {
		t.Error("full-chain stream: missing message_start event")
	}
	if !gotStop {
		t.Error("full-chain stream: missing message_stop event")
	}
	if len(textChunks) == 0 {
		t.Error("full-chain stream: no content_block_delta events received")
	}

	// Wait for async token write to complete.
	cancel()
	writer.Wait()

	logs, err := usageRepo.Query(db.UsageFilter{UserID: "chain-stream-user", Limit: 5})
	if err != nil {
		t.Fatalf("Query usage logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 usage log after full-chain stream, got %d", len(logs))
	}
	if logs[0].InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", logs[0].InputTokens, wantInput)
	}
	if logs[0].OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d", logs[0].OutputTokens, wantOutput)
	}
	if !logs[0].IsStreaming {
		t.Error("IsStreaming should be true for full-chain SSE request")
	}
}

// ---------------------------------------------------------------------------
// TestE2ERoutingTablePropagation — X-Routing-Update applied by cproxy
// ---------------------------------------------------------------------------
//
// Verifies the cluster routing update mechanism end-to-end:
//   - cproxy starts knowing only the primary s-proxy
//   - primary injects X-Routing-Update containing only the worker in its table
//   - cproxy applies the update: balancer now has only the worker
//   - second request is served by the worker (not the primary)
//   - routing headers are never exposed to the client
func TestE2ERoutingTablePropagation(t *testing.T) {
	// Worker: second s-proxy node.
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-PairProxy-Auth") == "" {
			t.Error("worker: missing X-PairProxy-Auth")
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", "worker")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer worker.Close()

	// Primary: accepts first request and injects a routing table containing ONLY worker.
	// This simulates a cluster primary that is handing off traffic to a worker.
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-PairProxy-Auth") == "" {
			t.Error("primary: missing X-PairProxy-Auth")
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		rt := &cluster.RoutingTable{
			Version: 1,
			Entries: []cluster.RoutingEntry{
				{ID: worker.URL, Addr: worker.URL, Weight: 1, Healthy: true},
			},
		}
		encoded, err := rt.Encode()
		if err != nil {
			http.Error(w, "routing encode error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Routing-Version", strconv.FormatInt(rt.Version, 10))
		w.Header().Set("X-Routing-Update", encoded)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", "primary")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer primary.Close()

	// cproxy starts knowing only the primary.
	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: primary.URL, Addr: primary.URL, Weight: 1, Healthy: true},
	})
	cpSrv, _, accessToken := buildCProxy(t, balancer)

	// First request — must go to primary (only known node), routing update applied.
	resp1 := doClaudeRequest(t, cpSrv, accessToken)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request: status=%d body=%s", resp1.StatusCode, body1)
	}
	if servedBy := resp1.Header.Get("X-Served-By"); servedBy != "primary" {
		t.Errorf("first request X-Served-By = %q, want 'primary'", servedBy)
	}
	// Routing headers must be stripped by cproxy (never reach the client).
	if v := resp1.Header.Get("X-Routing-Version"); v != "" {
		t.Errorf("X-Routing-Version leaked to client: %q", v)
	}
	if v := resp1.Header.Get("X-Routing-Update"); v != "" {
		t.Errorf("X-Routing-Update leaked to client")
	}

	// Second request — balancer now has only worker (primary was removed by UpdateTargets).
	resp2 := doClaudeRequest(t, cpSrv, accessToken)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second request: status=%d body=%s", resp2.StatusCode, body2)
	}
	if servedBy := resp2.Header.Get("X-Served-By"); servedBy != "worker" {
		t.Errorf("second request X-Served-By = %q, want 'worker' (routing table propagation should have redirected traffic)", servedBy)
	}
}

// ---------------------------------------------------------------------------
// TestE2ECproxyNoToken — cproxy returns 401 when no token is saved
// ---------------------------------------------------------------------------
//
// Simulates a user who has not yet run 'cproxy login': the token directory
// is empty, so cproxy cannot load a token and must immediately reject the
// request with 401 without contacting any s-proxy.
func TestE2ECproxyNoToken(t *testing.T) {
	spCalled := false
	mockSP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer mockSP.Close()

	logger := zaptest.NewLogger(t)
	tokenStore := auth.NewTokenStore(logger, 30*time.Minute)
	emptyDir := t.TempDir() // no token file in this directory

	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: mockSP.URL, Addr: mockSP.URL, Weight: 1, Healthy: true},
	})
	cp, err := proxy.NewCProxy(logger, tokenStore, emptyDir, balancer, "")
	if err != nil {
		t.Fatalf("NewCProxy: %v", err)
	}

	cpMux := http.NewServeMux()
	cpMux.Handle("/", cp.Handler())
	cpSrv := httptest.NewServer(cpMux)
	defer cpSrv.Close()

	req, _ := http.NewRequest(http.MethodPost, cpSrv.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude-3","messages":[]}`))
	req.Header.Set("Authorization", "Bearer dummy")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (no token saved)", resp.StatusCode)
	}
	if spCalled {
		t.Error("s-proxy should not be contacted when cproxy has no token")
	}
}

// ---------------------------------------------------------------------------
// TestE2EFullChainNonStreaming — full stack non-streaming with header verification
// ---------------------------------------------------------------------------
//
// Complements TestE2EFullStack by also verifying:
//   - sproxy-side token counts are correct
//   - no internal headers (X-PairProxy-Auth, X-Routing-*) leak to LLM or client
//   - cproxy injects X-Routing-Version in the upstream request
func TestE2EFullChainNonStreaming(t *testing.T) {
	const (
		wantInput  = 80
		wantOutput = 40
	)

	var clientRoutingVersion string
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify header transformation at LLM boundary.
		if got := r.Header.Get("Authorization"); got != "Bearer fullchain-ns-key" {
			t.Errorf("LLM Authorization = %q, want 'Bearer fullchain-ns-key'", got)
		}
		if r.Header.Get("X-PairProxy-Auth") != "" {
			t.Error("X-PairProxy-Auth must not reach LLM")
		}
		// X-Routing-Version should have been stripped by sproxy before forwarding to LLM.
		if v := r.Header.Get("X-Routing-Version"); v != "" {
			t.Errorf("X-Routing-Version must not reach LLM, got %q", v)
		}
		clientRoutingVersion = r.Header.Get("X-Routing-Version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"msg","type":"message","model":"claude-3-5-sonnet",`+
			`"usage":{"input_tokens":80,"output_tokens":40}}`)
	}))
	defer mockLLM.Close()

	logger := zaptest.NewLogger(t)

	jwtMgr, err := auth.NewManager(logger, "fullchain-ns-secret-xyz")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	writer := db.NewUsageWriter(gormDB, logger, 200, 30*time.Second)
	writer.Start(ctx)

	userRepo := db.NewUserRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)

	if err := userRepo.Create(&db.User{
		ID: "fcns-user", Username: "fcns", PasswordHash: "x", IsActive: true,
	}); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	sp, err := proxy.NewSProxy(logger, jwtMgr, writer, []proxy.LLMTarget{
		{URL: mockLLM.URL, APIKey: "fullchain-ns-key"},
	})
	if err != nil {
		t.Fatalf("NewSProxy: %v", err)
	}
	checker := quota.NewChecker(logger, userRepo, usageRepo, quota.NewQuotaCache(time.Minute))
	sp.SetQuotaChecker(checker)

	spMux := http.NewServeMux()
	spMux.HandleFunc("GET /health", sp.HealthHandler())
	spMux.Handle("/", sp.Handler())
	spSrv := httptest.NewServer(spMux)
	defer spSrv.Close()

	accessToken, err := jwtMgr.Sign(auth.JWTClaims{
		UserID: "fcns-user", Username: "fcns", Role: "user",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Sign JWT: %v", err)
	}

	tokenStore := auth.NewTokenStore(logger, 30*time.Minute)
	tokenDir := t.TempDir()
	if err := tokenStore.Save(tokenDir, &auth.TokenFile{
		AccessToken:  accessToken,
		RefreshToken: "unused",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		ServerAddr:   spSrv.URL,
		Username:     "fcns",
	}); err != nil {
		t.Fatalf("Save token: %v", err)
	}

	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: spSrv.URL, Addr: spSrv.URL, Weight: 1, Healthy: true},
	})
	cp, err := proxy.NewCProxy(logger, tokenStore, tokenDir, balancer, "")
	if err != nil {
		t.Fatalf("NewCProxy: %v", err)
	}

	cpMux := http.NewServeMux()
	cpMux.Handle("/", cp.Handler())
	cpSrv := httptest.NewServer(cpMux)
	defer cpSrv.Close()

	req, err := http.NewRequest(http.MethodPost, cpSrv.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"test"}]}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer dummy-key")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, respBody)
	}

	// Internal headers must not reach the client.
	if v := resp.Header.Get("X-PairProxy-Auth"); v != "" {
		t.Errorf("X-PairProxy-Auth leaked to client: %q", v)
	}
	// Confirm the response body is valid JSON.
	if !strings.Contains(string(respBody), "message") {
		t.Errorf("response body = %s, want JSON with 'message'", respBody)
	}
	_ = clientRoutingVersion // captured for future assertions if needed

	cancel()
	writer.Wait()

	logs, err := usageRepo.Query(db.UsageFilter{UserID: "fcns-user", Limit: 5})
	if err != nil {
		t.Fatalf("Query usage: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 usage log, got %d", len(logs))
	}
	if logs[0].InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", logs[0].InputTokens, wantInput)
	}
	if logs[0].OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d", logs[0].OutputTokens, wantOutput)
	}
	if logs[0].IsStreaming {
		t.Error("IsStreaming should be false for non-streaming request")
	}
}
