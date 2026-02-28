package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/db"
)

// newHealthTestSProxy 创建仅用于健康检查测试的 SProxy（无实际 LLM 后端）。
func newHealthTestSProxy(t *testing.T) *SProxy {
	t.Helper()
	logger := zaptest.NewLogger(t)

	// 创建最小化 JWT manager
	jwtMgr, err := auth.NewManager(logger, "test-secret-health")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	sp := &SProxy{
		logger:    logger.Named("sproxy"),
		jwtMgr:    jwtMgr,
		targets:   []LLMTarget{{URL: "http://unused", APIKey: "sk-test"}},
		transport: http.DefaultTransport,
		startTime: time.Now().Add(-10 * time.Minute), // 假设已运行 10 分钟
	}
	return sp
}

// ---------------------------------------------------------------------------
// TestHealthHandler_OK — 正常状态（无 DB ping）
// ---------------------------------------------------------------------------

func TestHealthHandler_OK(t *testing.T) {
	sp := newHealthTestSProxy(t)
	// 不设置 writer 和 sqlDB，验证 nil 安全性

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	sp.HealthHandler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	assertHealthField(t, resp, "status", "ok")
	assertHealthFieldPresent(t, resp, "version")
	assertHealthFieldPresent(t, resp, "uptime_seconds")
	assertHealthFieldPresent(t, resp, "active_requests")
	assertHealthFieldPresent(t, resp, "usage_queue_depth")
}

// ---------------------------------------------------------------------------
// TestHealthHandler_WithWriter — 验证 queue_depth 字段
// ---------------------------------------------------------------------------

func TestHealthHandler_WithWriter(t *testing.T) {
	sp := newHealthTestSProxy(t)
	logger := zaptest.NewLogger(t)

	// 创建内存 DB 和 UsageWriter
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	writer := db.NewUsageWriter(gormDB, logger, 100, time.Minute)
	writer.Start(ctx)
	defer func() {
		cancel()
		writer.Wait()
	}()

	sp.writer = writer

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	sp.HealthHandler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertHealthField(t, resp, "usage_queue_depth", float64(0))
}

// ---------------------------------------------------------------------------
// TestHealthHandler_DBReachable — 验证 db_reachable=true（正常 DB）
// ---------------------------------------------------------------------------

func TestHealthHandler_DBReachable(t *testing.T) {
	sp := newHealthTestSProxy(t)
	logger := zaptest.NewLogger(t)

	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	sp.SetDB(gormDB)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	sp.HealthHandler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertHealthField(t, resp, "status", "ok")
	assertHealthField(t, resp, "db_reachable", true)
}

// ---------------------------------------------------------------------------
// TestHealthHandler_UptimeIncreasing — 验证 uptime_seconds > 0
// ---------------------------------------------------------------------------

func TestHealthHandler_UptimeIncreasing(t *testing.T) {
	sp := newHealthTestSProxy(t)
	// startTime 已设置为 10 分钟前

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	sp.HealthHandler()(rr, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	uptimeRaw, ok := resp["uptime_seconds"]
	if !ok {
		t.Fatal("response missing uptime_seconds field")
	}
	uptime, ok := uptimeRaw.(float64)
	if !ok {
		t.Fatalf("uptime_seconds is not a number: %T", uptimeRaw)
	}
	if uptime < 600 { // 应至少 600s（10 分钟）
		t.Errorf("uptime_seconds = %v, want >= 600 (startTime was 10 min ago)", uptime)
	}
}

// ---------------------------------------------------------------------------
// TestHealthHandler_ActiveRequests — 验证 active_requests 计数
// ---------------------------------------------------------------------------

func TestHealthHandler_ActiveRequests(t *testing.T) {
	sp := newHealthTestSProxy(t)

	// 模拟正在处理请求
	sp.activeRequests.Add(3)
	defer sp.activeRequests.Add(-3)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	sp.HealthHandler()(rr, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertHealthField(t, resp, "active_requests", float64(3))
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

func assertHealthField(t *testing.T, resp map[string]interface{}, key string, want interface{}) {
	t.Helper()
	got, ok := resp[key]
	if !ok {
		t.Errorf("response missing field %q", key)
		return
	}
	if got != want {
		t.Errorf("resp[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

func assertHealthFieldPresent(t *testing.T, resp map[string]interface{}, key string) {
	t.Helper()
	if _, ok := resp[key]; !ok {
		t.Errorf("response missing field %q", key)
	}
}
