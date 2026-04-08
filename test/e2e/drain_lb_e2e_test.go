package e2e_test

// drain_lb_e2e_test.go — E2E tests for drain mode and load-balancing request distribution.
//
// Three scenarios not covered elsewhere:
//
//  1. TestE2ESProxyDrainTrafficSwitch  — drain-mode routing propagation via
//     X-Routing-Update headers: when a node marks itself draining in the routing
//     table, cproxy stops routing to it and redirects to healthy workers.
//
//  2. TestE2ECproxySkipsDrainingNode   — cproxy balancer with a Draining=true
//     node: Pick() skips it and all traffic goes to the healthy node.
//
//  3. TestE2ELLMLoadBalancing          — sproxy distributes requests across
//     multiple LLM backends (actual request routing, not just DB bindings).

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/cluster"
	"github.com/l17728/pairproxy/internal/db"
	"github.com/l17728/pairproxy/internal/lb"
	"github.com/l17728/pairproxy/internal/proxy"
)

// ---------------------------------------------------------------------------
// TestE2ESProxyDrainTrafficSwitch
// ---------------------------------------------------------------------------
//
// Scenario: one s-proxy node goes into drain mode mid-flight.
//
// The draining node:
//   - still serves the current request (返排水中仍可处理已进入的请求)
//   - injects a routing-table update that marks itself Draining=true and
//     introduces a healthy worker node
//
// After cproxy processes that routing update:
//   - the draining node is removed from Pick() candidates
//   - all subsequent requests reach the healthy worker
//
// Chain: Claude Code → cproxy → [drainingNode | worker]
func TestE2ESProxyDrainTrafficSwitch(t *testing.T) {
	// worker: healthy node that receives traffic after drain.
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-PairProxy-Auth") == "" {
			t.Error("worker: missing X-PairProxy-Auth")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", "worker")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer worker.Close()

	// drainingNode: processes requests but injects a routing table that marks
	// itself as Draining=true and adds the worker as a healthy target.
	// This is exactly what sproxy.Drain() + ClusterManager.DrainNode() produces.
	var drainingNodeURL string // filled after server starts
	drainingNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-PairProxy-Auth") == "" {
			t.Error("drainingNode: missing X-PairProxy-Auth")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Inject routing table: self is draining, worker is healthy.
		rt := &cluster.RoutingTable{
			Version: 1,
			Entries: []cluster.RoutingEntry{
				{ID: drainingNodeURL, Addr: drainingNodeURL, Weight: 1, Healthy: true, Draining: true},
				{ID: worker.URL, Addr: worker.URL, Weight: 1, Healthy: true, Draining: false},
			},
		}
		encoded, err := rt.Encode()
		if err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Routing-Version", strconv.FormatInt(rt.Version, 10))
		w.Header().Set("X-Routing-Update", encoded)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", "draining-node")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer drainingNode.Close()
	drainingNodeURL = drainingNode.URL

	// cproxy starts knowing only the draining node (before drain has occurred).
	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: drainingNode.URL, Addr: drainingNode.URL, Weight: 1, Healthy: true},
	})
	cpSrv, _, accessToken := buildCProxy(t, balancer)

	// Request 1: goes to drainingNode (only known target).
	// The response includes a routing update; cproxy applies it.
	resp1 := doClaudeRequest(t, cpSrv, accessToken)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("req1: status=%d body=%s", resp1.StatusCode, body1)
	}
	if got := resp1.Header.Get("X-Served-By"); got != "draining-node" {
		t.Errorf("req1 X-Served-By=%q, want 'draining-node'", got)
	}
	// Routing headers must be stripped by cproxy.
	if v := resp1.Header.Get("X-Routing-Version"); v != "" {
		t.Errorf("X-Routing-Version leaked to client after req1: %q", v)
	}

	// Requests 2–5: drainingNode is now marked Draining=true in cproxy's
	// balancer, so Pick() skips it. All traffic must go to worker.
	for i := 2; i <= 5; i++ {
		resp := doClaudeRequest(t, cpSrv, accessToken)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("req%d: status=%d body=%s", i, resp.StatusCode, body)
			continue
		}
		if got := resp.Header.Get("X-Served-By"); got != "worker" {
			t.Errorf("req%d X-Served-By=%q, want 'worker' (draining node must be excluded)", i, got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestE2ECproxySkipsDrainingNode
// ---------------------------------------------------------------------------
//
// Scenario: cproxy balancer is initialised with two targets, one of which
// already carries Draining=true (as would happen after applyRoutingTable()
// processes a drain event from the primary).
//
// Expected: Pick() never selects the draining node; 100 % of traffic goes to
// the healthy node.
func TestE2ECproxySkipsDrainingNode(t *testing.T) {
	var drainingHits, healthyHits atomic.Int64

	drainingNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainingHits.Add(1)
		w.Header().Set("X-Served-By", "draining")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message"}`)
	}))
	defer drainingNode.Close()

	healthyNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits.Add(1)
		w.Header().Set("X-Served-By", "healthy")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message"}`)
	}))
	defer healthyNode.Close()

	// Draining=true simulates the state after cproxy applied a routing-table
	// update that marked the primary as draining.
	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: drainingNode.URL, Addr: drainingNode.URL, Weight: 1, Healthy: true, Draining: true},
		{ID: healthyNode.URL, Addr: healthyNode.URL, Weight: 1, Healthy: true, Draining: false},
	})
	cpSrv, _, accessToken := buildCProxy(t, balancer)

	const n = 20
	for i := range n {
		resp := doClaudeRequest(t, cpSrv, accessToken)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("req %d: status=%d body=%s", i, resp.StatusCode, body)
		}
	}

	if drainingHits.Load() != 0 {
		t.Errorf("draining node received %d requests, want 0 (must be excluded by Pick())",
			drainingHits.Load())
	}
	if healthyHits.Load() != n {
		t.Errorf("healthy node received %d requests, want %d", healthyHits.Load(), n)
	}
	t.Logf("traffic distribution: draining=%d healthy=%d (out of %d)",
		drainingHits.Load(), healthyHits.Load(), n)
}

// ---------------------------------------------------------------------------
// TestE2ELLMLoadBalancing
// ---------------------------------------------------------------------------
//
// Scenario: sproxy is configured with two LLM backends of equal weight.
// 40 requests are sent through the full sproxy auth+proxy chain.
//
// Expected:
//   - All 40 requests succeed (200 OK).
//   - Both LLM backends receive traffic (weighted-random spread).
//   - Neither backend is starved (each handles ≥ 5 of 40 requests).
//
// This complements TestE2E_LLMDistribute_EvenSpread (which only tests the
// DB-binding helper) by verifying actual request routing.
func TestE2ELLMLoadBalancing(t *testing.T) {
	var llm1Hits, llm2Hits atomic.Int64

	mockLLM1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llm1Hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r","type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer mockLLM1.Close()

	mockLLM2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llm2Hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r","type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer mockLLM2.Close()

	logger := zaptest.NewLogger(t)
	jwtMgr, err := auth.NewManager(logger, "llm-lb-secret")
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
	writer := db.NewUsageWriter(gormDB, logger, 200, time.Minute)

	sp, err := proxy.NewSProxy(logger, jwtMgr, writer, []proxy.LLMTarget{
		{URL: mockLLM1.URL, APIKey: "k1", Weight: 1},
		{URL: mockLLM2.URL, APIKey: "k2", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewSProxy: %v", err)
	}

	// Wire up weighted-random LLM balancer (mirrors cmd/sproxy/main.go setup).
	lbTargets := []lb.Target{
		{ID: mockLLM1.URL, Addr: mockLLM1.URL, Weight: 1, Healthy: true},
		{ID: mockLLM2.URL, Addr: mockLLM2.URL, Weight: 1, Healthy: true},
	}
	bal := lb.NewWeightedRandom(lbTargets)
	hc := lb.NewHealthChecker(bal, logger, lb.WithFailThreshold(5))
	sp.SetLLMHealthChecker(bal, hc)

	srv := httptest.NewServer(sp.Handler())
	t.Cleanup(srv.Close)

	token, err := jwtMgr.Sign(auth.JWTClaims{UserID: "lb-user", Username: "lb"}, time.Hour)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Send 40 requests and collect results.
	const n = 40
	var failures int
	for i := range n {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages",
			bytes.NewBufferString(`{"model":"claude","messages":[]}`))
		req.Header.Set("X-PairProxy-Auth", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			failures++
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	h1, h2 := llm1Hits.Load(), llm2Hits.Load()
	t.Logf("LLM hit distribution: llm1=%d llm2=%d total=%d failures=%d", h1, h2, h1+h2, failures)

	if failures > 0 {
		t.Errorf("%d/%d requests failed (want 0)", failures, n)
	}
	if total := h1 + h2; total != n {
		t.Errorf("total LLM hits = %d, want %d", total, n)
	}
	// With equal weights and 40 requests, each LLM should get ≥ 5 hits
	// (probability of one getting < 5 is astronomically small).
	const minHits = 5
	if h1 < minHits {
		t.Errorf("llm1 only received %d/%d requests — load balancing may not be working", h1, n)
	}
	if h2 < minHits {
		t.Errorf("llm2 only received %d/%d requests — load balancing may not be working", h2, n)
	}
}

// ---------------------------------------------------------------------------
// TestE2ELLMFailoverOnConnectionError
// ---------------------------------------------------------------------------
//
// Scenario: LLM-1 is down (connection refused); LLM-2 is healthy.
// sproxy's RetryTransport should transparently retry on LLM-2.
//
// Uses real httptest servers (rather than fake URLs) so the connection-refused
// error is genuine.
func TestE2ELLMFailoverOnConnectionError(t *testing.T) {
	var llm2Hits atomic.Int64

	mockLLM2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llm2Hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r","type":"message","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer mockLLM2.Close()

	// LLM-1: start then immediately close → connection refused on any attempt.
	deadLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	deadURL := deadLLM.URL
	deadLLM.Close() // now unreachable

	logger := zaptest.NewLogger(t)
	jwtMgr, err := auth.NewManager(logger, "llm-failover-secret")
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
	writer := db.NewUsageWriter(gormDB, logger, 200, time.Minute)

	sp, err := proxy.NewSProxy(logger, jwtMgr, writer, []proxy.LLMTarget{
		{URL: deadURL, APIKey: "k1", Weight: 1},
		{URL: mockLLM2.URL, APIKey: "k2", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewSProxy: %v", err)
	}

	lbTargets := []lb.Target{
		{ID: deadURL, Addr: deadURL, Weight: 1, Healthy: true},
		{ID: mockLLM2.URL, Addr: mockLLM2.URL, Weight: 1, Healthy: true},
	}
	bal := lb.NewWeightedRandom(lbTargets)
	hc := lb.NewHealthChecker(bal, logger, lb.WithFailThreshold(3))
	sp.SetLLMHealthChecker(bal, hc)
	sp.SetMaxRetries(2) // allow retry to second target

	srv := httptest.NewServer(sp.Handler())
	t.Cleanup(srv.Close)

	token, err := jwtMgr.Sign(auth.JWTClaims{UserID: "fo-user", Username: "fo"}, time.Hour)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Send 3 requests; retry transport should always land on LLM-2.
	for i := range 3 {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages",
			bytes.NewBufferString(`{"model":"claude","messages":[]}`))
		req.Header.Set("X-PairProxy-Auth", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("req %d: status=%d body=%s (retry to llm2 should have succeeded)", i, resp.StatusCode, body)
		}
	}

	if llm2Hits.Load() < 3 {
		t.Errorf("llm2 received %d requests, want ≥3 (retry should always land on llm2)", llm2Hits.Load())
	}
	t.Logf("llm2 hit %d times (dead llm retried away transparently)", llm2Hits.Load())
}

// ---------------------------------------------------------------------------
// TestE2EUndrain — drain then undrain restores traffic
// ---------------------------------------------------------------------------
//
// Scenario:
//  1. cproxy knows drainingNode + worker, both healthy.
//  2. drainingNode's first response marks itself Draining=true (routing update).
//  3. Requests 2–4 all reach worker (drainingNode excluded).
//  4. A second routing update re-introduces drainingNode as Draining=false.
//  5. Requests 5–6 can again reach drainingNode.
func TestE2EUndrain(t *testing.T) {
	// Phase 1: draining node marks itself draining.
	// Phase 2: after "undrain", a subsequent response marks it healthy again.
	phase2Rt := &cluster.RoutingTable{
		Version: 2, // higher than phase-1 version (1)
		Entries: []cluster.RoutingEntry{
			// both nodes healthy, not draining
			{ID: "draining-node", Addr: "", Weight: 1, Healthy: true, Draining: false},
			{ID: "worker", Addr: "", Weight: 1, Healthy: true, Draining: false},
		},
	}

	var workerURL, drainingURL string
	var requestCount atomic.Int64

	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		w.Header().Set("X-Served-By", "worker")
		// On the 4th overall request (3rd after initial drain), inject undrain update.
		if n == 4 {
			phase2Rt.Entries[0].Addr = drainingURL
			phase2Rt.Entries[1].Addr = workerURL
			encoded, _ := phase2Rt.Encode()
			w.Header().Set("X-Routing-Version", "2")
			w.Header().Set("X-Routing-Update", encoded)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer worker.Close()
	workerURL = worker.URL
	// Update IDs after URL is known.
	phase2Rt.Entries[1].ID = workerURL

	var drainingNode *httptest.Server
	drainingNode = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		drainingURL = drainingNode.URL
		rt := &cluster.RoutingTable{
			Version: 1,
			Entries: []cluster.RoutingEntry{
				{ID: drainingURL, Addr: drainingURL, Weight: 1, Healthy: true, Draining: true},
				{ID: workerURL, Addr: workerURL, Weight: 1, Healthy: true, Draining: false},
			},
		}
		phase2Rt.Entries[0].ID = drainingURL
		encoded, _ := rt.Encode()
		w.Header().Set("X-Routing-Version", "1")
		w.Header().Set("X-Routing-Update", encoded)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", "draining-node")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer drainingNode.Close()
	drainingURL = drainingNode.URL

	// cproxy starts knowing only the draining node.
	balancer := lb.NewWeightedRandom([]lb.Target{
		{ID: drainingNode.URL, Addr: drainingNode.URL, Weight: 1, Healthy: true},
	})
	cpSrv, cp, _, accessToken := buildCProxyWithInstance(t, balancer)

	// req1 → drainingNode (drain routing update applied)
	resp1 := doClaudeRequest(t, cpSrv, accessToken)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("req1: status=%d body=%s", resp1.StatusCode, body1)
	}

	// req2–4 → worker only (drainingNode excluded)
	for i := 2; i <= 4; i++ {
		resp := doClaudeRequest(t, cpSrv, accessToken)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("req%d: status=%d", i, resp.StatusCode)
		}
		if got := resp.Header.Get("X-Served-By"); got != "worker" {
			t.Errorf("req%d X-Served-By=%q, want 'worker' (drained node must be excluded)", i, got)
		}
	}
	// req4 from worker injects the undrain routing update (version=2).
	// After this, drainingNode is healthy again.

	// Wait for the routing version to be applied (version=2) before sending more requests.
	// In -race mode, this propagation can be slow, so we poll with a reasonable timeout.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cp.RoutingVersion() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cp.RoutingVersion() < 2 {
		t.Logf("warning: routing version did not reach 2 within timeout (got %d)", cp.RoutingVersion())
	}

	// req5–req14: both nodes should be eligible; verify drainingNode is reachable again
	// by checking that at least one request is served by it in 10 attempts.
	sawDrainingNodeAgain := false
	for i := 5; i <= 14; i++ {
		resp := doClaudeRequest(t, cpSrv, accessToken)
		body, _ := io.ReadAll(resp.Body)
		served := resp.Header.Get("X-Served-By")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("req%d: status=%d body=%s", i, resp.StatusCode, body)
		}
		if served == "draining-node" {
			sawDrainingNodeAgain = true
		}
	}
	if !sawDrainingNodeAgain {
		t.Error("after undrain, drainingNode never received a request in 10 tries (undrain may not have worked)")
	}
}

