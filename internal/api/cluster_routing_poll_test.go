package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/cluster"
	"github.com/l17728/pairproxy/internal/lb"
)

// buildTestClusterManager 创建用于测试的 Manager。
func buildTestClusterManager(t *testing.T) *cluster.Manager {
	t.Helper()
	logger := zaptest.NewLogger(t)
	targets := []lb.Target{
		{ID: "sp-2", Addr: "http://sp-2:9000", Weight: 1, Healthy: true},
	}
	balancer := lb.NewWeightedRandom(targets)
	return cluster.NewManager(logger, balancer, targets, "")
}

// TestHandleRoutingPoll_NoAuth 测试未认证时返回 401。
func TestHandleRoutingPoll_NoAuth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	h.SetManager(buildTestClusterManager(t))

	req := httptest.NewRequest(http.MethodGet, "/cluster/routing-poll", nil)
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestHandleRoutingPoll_WrongMethod 测试非 GET 方法返回 405。
func TestHandleRoutingPoll_WrongMethod(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	h.SetManager(buildTestClusterManager(t))

	req := httptest.NewRequest(http.MethodPost, "/cluster/routing-poll", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// TestHandleRoutingPoll_NoManager 测试未设置 Manager 时返回 404。
func TestHandleRoutingPoll_NoManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	// 不调用 SetManager

	req := httptest.NewRequest(http.MethodGet, "/cluster/routing-poll", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestHandleRoutingPoll_ClientUpToDate 测试客户端版本已是最新时返回 304。
func TestHandleRoutingPoll_ClientUpToDate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	mgr := buildTestClusterManager(t)
	h.SetManager(mgr)

	// 获取当前版本
	rt := mgr.CurrentTable()
	currentVersion := rt.Version

	req := httptest.NewRequest(http.MethodGet, "/cluster/routing-poll", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	req.Header.Set("X-Routing-Version", strconv.FormatInt(currentVersion, 10))
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	if w.Code != http.StatusNotModified {
		t.Errorf("expected 304 when client is up to date, got %d", w.Code)
	}
}

// TestHandleRoutingPoll_ClientStale 测试客户端版本过旧时返回 200 + 路由表。
func TestHandleRoutingPoll_ClientStale(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	mgr := buildTestClusterManager(t)
	h.SetManager(mgr)

	// 客户端版本为 0（过旧）
	req := httptest.NewRequest(http.MethodGet, "/cluster/routing-poll", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	req.Header.Set("X-Routing-Version", "0")
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when client is stale, got %d", w.Code)
	}

	// 验证响应头包含路由表
	if w.Header().Get("X-Routing-Version") == "" {
		t.Error("expected X-Routing-Version header in response")
	}
	if w.Header().Get("X-Routing-Update") == "" {
		t.Error("expected X-Routing-Update header in response")
	}

	// 验证路由表可以解码
	encoded := w.Header().Get("X-Routing-Update")
	rt, err := cluster.DecodeRoutingTable(encoded)
	if err != nil {
		t.Fatalf("failed to decode routing table: %v", err)
	}
	if len(rt.Entries) == 0 {
		t.Error("expected at least one routing entry")
	}
}

// TestHandleRoutingPoll_NoVersionHeader 测试无版本头时（版本=0）返回路由表。
func TestHandleRoutingPoll_NoVersionHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")
	mgr := buildTestClusterManager(t)
	h.SetManager(mgr)

	req := httptest.NewRequest(http.MethodGet, "/cluster/routing-poll", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	// 不设置 X-Routing-Version
	w := httptest.NewRecorder()
	h.handleRoutingPoll(w, req)

	// 版本 0 < 当前版本，应该返回路由表
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no version header, got %d", w.Code)
	}
}

// TestSetManager 测试 SetManager 方法。
func TestSetManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h := NewClusterHandler(logger, nil, nil, "secret-key")

	if h.manager != nil {
		t.Error("expected nil manager initially")
	}

	mgr := buildTestClusterManager(t)
	h.SetManager(mgr)

	if h.manager == nil {
		t.Error("expected non-nil manager after SetManager")
	}
}
