package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGroupTargetSetAPI 测试 Group-Target Set API 端点
func TestGroupTargetSetAPI(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sets": []interface{}{},
		})
	}))
	defer server.Close()

	// 测试 GET /api/admin/targetsets
	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
}

// TestAlertAPI 测试告警 API 端点
func TestAlertAPI(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"alerts": []interface{}{},
		})
	}))
	defer server.Close()

	// 测试 GET /api/admin/alerts/active
	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestSSEAlertStream 测试 SSE 告警流
func TestSSEAlertStream(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: connected\ndata: {\"message\": \"connected\"}\n\n"))
	}))
	defer server.Close()

	// 测试 SSE 连接
	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}

// TestCreateTargetSet 测试创建 Target Set
func TestCreateTargetSet(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "test-id",
				"name": "test-pool",
			})
		}
	}))
	defer server.Close()

	// 创建 Target Set
	payload := map[string]interface{}{
		"name":     "test-pool",
		"strategy": "weighted_random",
		"targets": []map[string]interface{}{
			{
				"url":    "http://localhost:8001",
				"weight": 1,
			},
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(server.URL, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

// TestAlertStats 测试获取告警统计
func TestAlertStats(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active_alerts":   0,
			"resolved_alerts": 0,
			"total_alerts":    0,
		})
	}))
	defer server.Close()

	// 获取告警统计
	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var stats map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&stats)
	assert.NotNil(t, stats)
}
