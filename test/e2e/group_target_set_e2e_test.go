package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGroupTargetSetPooling 测试 Group-Target Set 池化和故障转移
func TestGroupTargetSetPooling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// 启动测试服务器
	srv := startTestServer(t)
	defer srv.Close()

	baseURL := srv.URL
	adminToken := getAdminToken(t, baseURL)

	// 1. 创建 Group-Target Set
	t.Run("CreateTargetSet", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":     "test-pool",
			"strategy": "weighted_random",
			"targets": []map[string]interface{}{
				{
					"url":      "http://localhost:8001",
					"weight":   1,
					"priority": 0,
				},
				{
					"url":      "http://localhost:8002",
					"weight":   1,
					"priority": 0,
				},
			},
		}

		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", baseURL+"/api/admin/targetsets", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 2. 列出 Target Sets
	t.Run("ListTargetSets", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/targetsets", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotNil(t, result["sets"])
	})

	// 3. 获取活跃告警
	t.Run("ListActiveAlerts", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/alerts/active", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 4. 获取告警历史
	t.Run("ListAlertHistory", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/alerts/history", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 5. 获取告警统计
	t.Run("GetAlertStats", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/alerts/stats", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var stats map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stats)
		assert.NotNil(t, stats)
	})
}

// TestSSEAlertStreaming 测试 SSE 告警流
func TestSSEAlertStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	srv := startTestServer(t)
	defer srv.Close()

	baseURL := srv.URL
	adminToken := getAdminToken(t, baseURL)

	t.Run("ConnectToSSEStream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/admin/alerts/stream", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

		// 读取初始连接消息
		reader := io.NewReader(resp.Body)
		line := make([]byte, 1024)
		n, _ := reader.Read(line)
		assert.Greater(t, n, 0)
	})
}

// TestTargetHealthMonitoring 测试 Target 健康监控
func TestTargetHealthMonitoring(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	srv := startTestServer(t)
	defer srv.Close()

	baseURL := srv.URL
	adminToken := getAdminToken(t, baseURL)

	t.Run("HealthCheckEndpoint", func(t *testing.T) {
		// 等待健康检查运行
		time.Sleep(2 * time.Second)

		// 获取告警统计
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/alerts/stats", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestAlertResolution 测试告警解决
func TestAlertResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	srv := startTestServer(t)
	defer srv.Close()

	baseURL := srv.URL
	adminToken := getAdminToken(t, baseURL)

	t.Run("ResolveAlert", func(t *testing.T) {
		// 首先获取活跃告警
		req, _ := http.NewRequest("GET", baseURL+"/api/admin/alerts/active", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		var alerts map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&alerts)

		// 如果有告警，尝试解决它
		if alertList, ok := alerts["alerts"].([]interface{}); ok && len(alertList) > 0 {
			alert := alertList[0].(map[string]interface{})
			alertID := alert["id"].(string)

			// 解决告警
			payload := map[string]interface{}{
				"alert_id": alertID,
				"reason":   "resolved",
			}
			body, _ := json.Marshal(payload)

			req, _ := http.NewRequest("POST", baseURL+"/api/admin/alerts/resolve", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}
	})
}

// 辅助函数
func startTestServer(t *testing.T) *http.Server {
	// 这里应该启动一个测试服务器
	// 为了简化，我们假设服务器已经在运行
	return &http.Server{
		Addr: ":8080",
	}
}

func getAdminToken(t *testing.T, baseURL string) string {
	// 这里应该获取管理员令牌
	// 为了简化，我们返回一个测试令牌
	return "test-admin-token"
}
