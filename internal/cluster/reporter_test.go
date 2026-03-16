package cluster

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/db"
)

func TestReporterHeartbeat(t *testing.T) {
	var (
		mu       sync.Mutex
		received []RegisterPayload
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != defaultRegisterPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var p RegisterPayload
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	r := NewReporter(logger, ReporterConfig{
		SP1Addr:    srv.URL,
		SelfID:     "sp-2",
		SelfAddr:   "http://sp-2:9000",
		SelfWeight: 2,
		Interval:   50 * time.Millisecond,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	r.Start(ctx)
	t.Cleanup(func() {
		cancel()
		r.Wait()
	})

	// 等待至少 2 个心跳（启动时立即 + 50ms 后）
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	count := len(received)
	var first RegisterPayload
	if count > 0 {
		first = received[0]
	}
	mu.Unlock()

	if count < 2 {
		t.Errorf("expected ≥2 heartbeats, got %d", count)
	}
	if first.ID != "sp-2" || first.Addr != "http://sp-2:9000" {
		t.Errorf("unexpected payload: %+v", first)
	}
}

func TestReporterUsageReport(t *testing.T) {
	var receivedPayload UsageReportPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != defaultUsageReportPath {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  srv.URL,
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
	}, nil)

	records := []db.UsageRecord{
		{RequestID: "req-1", UserID: "user-1", InputTokens: 100, OutputTokens: 50},
		{RequestID: "req-2", UserID: "user-2", InputTokens: 200, OutputTokens: 80},
	}

	if err := reporter.ReportUsage(context.Background(), records); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}

	if receivedPayload.SourceNode != "sp-2" {
		t.Errorf("SourceNode = %q, want 'sp-2'", receivedPayload.SourceNode)
	}
	if len(receivedPayload.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(receivedPayload.Records))
	}
	if receivedPayload.Records[0].InputTokens != 100 {
		t.Errorf("Records[0].InputTokens = %d, want 100", receivedPayload.Records[0].InputTokens)
	}
}

func TestReporterHeartbeatAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-secret" {
			t.Errorf("Authorization = %q, want 'Bearer my-secret'", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:      srv.URL,
		SelfID:       "sp-2",
		SelfAddr:     "http://sp-2:9000",
		SharedSecret: "my-secret",
		Interval:     1 * time.Hour, // 不触发定时器
	}, nil)

	// 只测试一次立即心跳
	reporter.sendHeartbeat(context.Background())
	// 测试不 panic，且 mock server 验证了 auth header
}

// ---------------------------------------------------------------------------
// TestHeartbeatFailures — HeartbeatFailures (0% coverage)
// ---------------------------------------------------------------------------

// TestReporter_HeartbeatFailures_ErrorServer 验证服务器返回 500 时 heartbeatFailures 增加。
func TestReporter_HeartbeatFailures_ErrorServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  srv.URL,
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	// 初始值为 0
	if f := reporter.HeartbeatFailures(); f != 0 {
		t.Errorf("initial HeartbeatFailures = %d, want 0", f)
	}

	reporter.sendHeartbeat(context.Background())

	if f := reporter.HeartbeatFailures(); f != 1 {
		t.Errorf("HeartbeatFailures = %d, want 1 after one failed heartbeat", f)
	}
}

// TestReporter_HeartbeatFailures_UnreachableServer 验证服务器不可达时 heartbeatFailures 增加。
func TestReporter_HeartbeatFailures_UnreachableServer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  "http://127.0.0.1:19998", // 不存在的端口
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	before := reporter.HeartbeatFailures()
	reporter.sendHeartbeat(context.Background())
	after := reporter.HeartbeatFailures()

	if after <= before {
		t.Errorf("HeartbeatFailures should increase: before=%d after=%d", before, after)
	}
}

// TestReporter_HeartbeatFailures_Cumulative 验证多次失败后计数累积。
func TestReporter_HeartbeatFailures_Cumulative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  srv.URL,
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	for i := 0; i < 3; i++ {
		reporter.sendHeartbeat(context.Background())
	}

	if f := reporter.HeartbeatFailures(); f != 3 {
		t.Errorf("HeartbeatFailures = %d, want 3 after 3 failures", f)
	}
}

// ---------------------------------------------------------------------------
// TestLastLatencyMs — LastLatencyMs (0% coverage)
// ---------------------------------------------------------------------------

// TestReporter_LastLatencyMs_InitiallyMinusOne 验证初始值为 -1（从未成功）。
func TestReporter_LastLatencyMs_InitiallyMinusOne(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  "http://127.0.0.1:19997",
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	if latency := reporter.LastLatencyMs(); latency != -1 {
		t.Errorf("LastLatencyMs() = %d, want -1 before any heartbeat", latency)
	}
}

// TestReporter_LastLatencyMs_UpdatedAfterSuccess 验证成功心跳后 lastLatencyMs > 0。
func TestReporter_LastLatencyMs_UpdatedAfterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  srv.URL,
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	// 成功发送一次心跳
	reporter.sendHeartbeat(context.Background())

	latency := reporter.LastLatencyMs()
	if latency < 0 {
		t.Errorf("LastLatencyMs() = %d after successful heartbeat, want ≥0", latency)
	}
}

// TestReporter_LastLatencyMs_NotUpdatedOnFailure 验证失败心跳不会更新 lastLatencyMs。
func TestReporter_LastLatencyMs_NotUpdatedOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	logger := zaptest.NewLogger(t)
	reporter := NewReporter(logger, ReporterConfig{
		SP1Addr:  srv.URL,
		SelfID:   "sp-2",
		SelfAddr: "http://sp-2:9000",
		Interval: 1 * time.Hour,
	}, nil)

	// 失败心跳后 lastLatencyMs 应仍为 -1
	reporter.sendHeartbeat(context.Background())

	if latency := reporter.LastLatencyMs(); latency != -1 {
		t.Errorf("LastLatencyMs() = %d after failed heartbeat, want -1 (unchanged)", latency)
	}
}
