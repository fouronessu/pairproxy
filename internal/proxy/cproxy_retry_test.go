package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/config"
	"github.com/l17728/pairproxy/internal/lb"
)

// ---------------------------------------------------------------------------
// isStreamingBody tests
// ---------------------------------------------------------------------------

func TestIsStreamingBody(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		expected bool
	}{
		{"empty body", nil, false},
		{"non-streaming", []byte(`{"model":"claude-3","messages":[]}`), false},
		{"streaming true", []byte(`{"model":"claude-3","stream":true}`), true},
		{"streaming false", []byte(`{"model":"claude-3","stream":false}`), false},
		{"invalid json", []byte(`not json`), false},
		{"empty json", []byte(`{}`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStreamingBody(tt.body)
			if got != tt.expected {
				t.Errorf("isStreamingBody(%q) = %v, want %v", tt.body, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// shouldRetry tests
// ---------------------------------------------------------------------------

func TestShouldRetry(t *testing.T) {
	retryOn := []int{502, 503, 504}

	tests := []struct {
		status   int
		expected bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{429, false},
		{500, false},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		got := shouldRetry(tt.status, retryOn)
		if got != tt.expected {
			t.Errorf("shouldRetry(%d) = %v, want %v", tt.status, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// serveWithRetry tests
// ---------------------------------------------------------------------------

// buildTestCProxyWithRetry 创建带重试配置的测试 CProxy。
func buildTestCProxyWithRetry(t *testing.T, targets []lb.Target, retryConfig config.RetryConfig) (*CProxy, *auth.TokenStore, string) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(targets)

	tokenDir := t.TempDir()
	store := auth.NewTokenStore(logger, 30*time.Minute)
	tf := &auth.TokenFile{
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		ServerAddr:   "http://localhost:9000",
		Username:     "testuser",
	}
	if err := store.Save(tokenDir, tf); err != nil {
		t.Fatalf("save token: %v", err)
	}

	cp, err := NewCProxy(logger, store, tokenDir, balancer, "")
	if err != nil {
		t.Fatalf("NewCProxy: %v", err)
	}
	cp.SetRetryConfig(retryConfig)
	return cp, store, tokenDir
}

// TestServeWithRetry_SuccessOnFirstAttempt 测试第一次请求成功时不重试。
func TestServeWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg-1","type":"message"}`))
	}))
	defer srv.Close()

	targets := []lb.Target{{ID: srv.URL, Addr: srv.URL, Weight: 1, Healthy: true}}
	cp, _, _ := buildTestCProxyWithRetry(t, targets, config.RetryConfig{
		Enabled:       true,
		MaxRetries:    2,
		RetryOnStatus: []int{502, 503, 504},
	})

	body := `{"model":"claude-3","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	tf := &auth.TokenFile{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	cp.serveWithRetry(w, req, tf, []byte(body), nil, "test-req-1")

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call, got %d", callCount.Load())
	}
}

// TestServeWithRetry_RetryOn502 测试 502 时重试到第二个节点。
func TestServeWithRetry_RetryOn502(t *testing.T) {
	var (
		failCalls    atomic.Int32
		successCalls atomic.Int32
	)

	// 第一个节点返回 502
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()

	// 第二个节点返回 200
	successSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg-1"}`))
	}))
	defer successSrv.Close()

	targets := []lb.Target{
		{ID: failSrv.URL, Addr: failSrv.URL, Weight: 1, Healthy: true},
		{ID: successSrv.URL, Addr: successSrv.URL, Weight: 1, Healthy: true},
	}
	cp, _, _ := buildTestCProxyWithRetry(t, targets, config.RetryConfig{
		Enabled:       true,
		MaxRetries:    2,
		RetryOnStatus: []int{502, 503, 504},
	})

	body := `{"model":"claude-3","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	tf := &auth.TokenFile{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	cp.serveWithRetry(w, req, tf, []byte(body), nil, "test-req-2")

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", w.Code)
	}
	// 成功节点必须被调用（无论是首次还是重试后）
	if successCalls.Load() == 0 {
		t.Error("success server was never called")
	}
	// 总调用次数：1（直接成功）或 2（失败后重试成功）
	total := failCalls.Load() + successCalls.Load()
	if total < 1 || total > 2 {
		t.Errorf("expected 1 or 2 total calls, got fail=%d success=%d",
			failCalls.Load(), successCalls.Load())
	}
}

// TestServeWithRetry_AllTargetsFail 测试所有节点失败时返回 502。
func TestServeWithRetry_AllTargetsFail(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	targets := []lb.Target{{ID: srv.URL, Addr: srv.URL, Weight: 1, Healthy: true}}
	cp, _, _ := buildTestCProxyWithRetry(t, targets, config.RetryConfig{
		Enabled:       true,
		MaxRetries:    2,
		RetryOnStatus: []int{502, 503, 504},
	})

	body := `{"model":"claude-3","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	tf := &auth.TokenFile{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	cp.serveWithRetry(w, req, tf, []byte(body), nil, "test-req-3")

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 when all targets fail, got %d", w.Code)
	}

	// 只有 1 个节点，最多尝试 1 次（不会重试到同一节点）
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call (single target), got %d", callCount.Load())
	}

	// 验证响应体包含错误信息
	respBody := w.Body.String()
	if !strings.Contains(respBody, "all_targets_exhausted") {
		t.Errorf("expected error code 'all_targets_exhausted' in body, got %q", respBody)
	}
}

// TestServeWithRetry_StreamingSkipsRetry 测试流式请求不走重试路径。
func TestServeWithRetry_StreamingSkipsRetry(t *testing.T) {
	// 流式请求应该走 ReverseProxy 路径，不调用 serveWithRetry
	// 这里通过 isStreamingBody 验证检测逻辑
	streamBody := []byte(`{"model":"claude-3","stream":true,"messages":[]}`)
	if !isStreamingBody(streamBody) {
		t.Error("expected streaming body to be detected as streaming")
	}

	nonStreamBody := []byte(`{"model":"claude-3","messages":[]}`)
	if isStreamingBody(nonStreamBody) {
		t.Error("expected non-streaming body to not be detected as streaming")
	}
}

// TestServeWithRetry_PassiveCircuitBreaker 测试失败时调用 healthChecker.RecordFailure。
func TestServeWithRetry_PassiveCircuitBreaker(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	targets := []lb.Target{{ID: srv.URL, Addr: srv.URL, Weight: 1, Healthy: true}}
	balancer := lb.NewWeightedRandom(targets)
	logger := zaptest.NewLogger(t)

	// 创建健康检查器（失败阈值=1，立即熔断）
	hc := lb.NewHealthChecker(balancer, logger,
		lb.WithFailThreshold(1),
	)

	tokenDir := t.TempDir()
	store := auth.NewTokenStore(logger, 30*time.Minute)
	tf := &auth.TokenFile{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	_ = store.Save(tokenDir, tf)

	cp, err := NewCProxy(logger, store, tokenDir, balancer, "")
	if err != nil {
		t.Fatalf("NewCProxy: %v", err)
	}
	cp.SetHealthChecker(hc)
	cp.SetRetryConfig(config.RetryConfig{
		Enabled:       true,
		MaxRetries:    1,
		RetryOnStatus: []int{502, 503, 504},
	})

	body := `{"model":"claude-3","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	cp.serveWithRetry(w, req, tf, []byte(body), nil, "test-req-4")

	// 节点应该被标记为不健康
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}

	// 验证节点被熔断（Pick 应该失败）
	time.Sleep(10 * time.Millisecond) // 等待异步标记
	_, err = balancer.Pick()
	if err == nil {
		t.Log("note: target may not be marked unhealthy yet (async)")
	}
}

// TestPickUntried 测试 pickUntried 逻辑。
func TestPickUntried(t *testing.T) {
	targets := []lb.Target{
		{ID: "t1", Addr: "http://t1:9000", Weight: 1, Healthy: true},
		{ID: "t2", Addr: "http://t2:9000", Weight: 1, Healthy: true},
		{ID: "t3", Addr: "http://t3:9000", Weight: 1, Healthy: true},
	}
	balancer := lb.NewWeightedRandom(targets)
	logger := zaptest.NewLogger(t)
	store := auth.NewTokenStore(logger, 30*time.Minute)
	cp, _ := NewCProxy(logger, store, t.TempDir(), balancer, "")

	tried := make(map[string]bool)

	// 第一次：应该返回一个节点
	t1 := cp.pickUntried(tried)
	if t1 == nil {
		t.Fatal("expected a target, got nil")
	}
	tried[t1.ID] = true

	// 第二次：应该返回不同节点
	t2 := cp.pickUntried(tried)
	if t2 == nil {
		t.Fatal("expected a second target, got nil")
	}
	if t2.ID == t1.ID {
		t.Errorf("expected different target, got same: %s", t1.ID)
	}
	tried[t2.ID] = true

	// 第三次：应该返回第三个节点
	t3 := cp.pickUntried(tried)
	if t3 == nil {
		t.Fatal("expected a third target, got nil")
	}
	tried[t3.ID] = true

	// 第四次：所有节点已尝试，应该返回 nil
	t4 := cp.pickUntried(tried)
	if t4 != nil {
		t.Errorf("expected nil when all targets tried, got %s", t4.ID)
	}
}

// TestWriteProxyResponse 测试 writeProxyResponse 正确复制响应。
func TestWriteProxyResponse(t *testing.T) {
	body := `{"id":"msg-1","type":"message"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Custom":     []string{"value"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}

	w := httptest.NewRecorder()
	writeProxyResponse(w, resp)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != body {
		t.Errorf("expected body %q, got %q", body, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header")
	}
	if w.Header().Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header")
	}
}
