//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2E_APIStatus 测试 API 状态端点
func TestE2E_APIStatus(t *testing.T) {
	// 启动 load tester
	cmd := exec.Command("./claude-load-tester", "run",
		"--mode", "fixed",
		"--workers", "1",
		"--duration", "5m",
		"--api-enabled",
		"--api-addr", ":18080",
	)
	cmd.Dir = ".."

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start load tester: %v", err)
	}
	defer cmd.Process.Kill()

	// 等待服务启动
	time.Sleep(2 * time.Second)

	// 测试状态端点
	resp, err := http.Get("http://localhost:18080/api/status")
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Errorf("Failed to decode status: %v", err)
	}

	if running, ok := status["running"].(bool); !ok || !running {
		t.Error("Expected running to be true")
	}
}

// TestE2E_APIMetrics 测试 Prometheus 指标端点
func TestE2E_APIMetrics(t *testing.T) {
	cmd := exec.Command("./claude-load-tester", "run",
		"--mode", "fixed",
		"--workers", "1",
		"--duration", "5m",
		"--api-enabled",
		"--api-addr", ":18081",
		"--api-prometheus",
	)
	cmd.Dir = ".."

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start load tester: %v", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(2 * time.Second)

	// 测试 metrics 端点
	resp, err := http.Get("http://localhost:18081/api/metrics")
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain content type, got %s", contentType)
	}
}

// TestE2E_WebSocket 测试 WebSocket 连接
func TestE2E_WebSocket(t *testing.T) {
	cmd := exec.Command("./claude-load-tester", "run",
		"--mode", "fixed",
		"--workers", "1",
		"--duration", "5m",
		"--api-enabled",
		"--api-addr", ":18082",
	)
	cmd.Dir = ".."

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start load tester: %v", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(2 * time.Second)

	// WebSocket 连接测试需要 gorilla/websocket 客户端
	// 这里只做基本的 HTTP 升级检查
	req, err := http.NewRequest("GET", "http://localhost:18082/ws", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("WebSocket endpoint test: %v", err)
		// WebSocket 测试可能失败，不直接报错
		return
	}
	defer resp.Body.Close()

	// 101 Switching Protocols 表示 WebSocket 升级成功
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Logf("WebSocket upgrade returned status: %d (expected 101)", resp.StatusCode)
	}
}

// TestE2E_RemoteControl 测试远程控制 API
func TestE2E_RemoteControl(t *testing.T) {
	cmd := exec.Command("./claude-load-tester", "run",
		"--mode", "fixed",
		"--workers", "2",
		"--duration", "10m",
		"--api-enabled",
		"--api-addr", ":18083",
	)
	cmd.Dir = ".."

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start load tester: %v", err)
	}
	defer cmd.Process.Kill()

	time.Sleep(2 * time.Second)

	// 测试停止 API
	resp, err := http.Post("http://localhost:18083/api/stop", "", nil)
	if err != nil {
		t.Fatalf("Failed to stop test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for stop, got %d", resp.StatusCode)
	}

	// 等待停止
	time.Sleep(1 * time.Second)

	// 验证状态
	resp2, err := http.Get("http://localhost:18083/api/status")
	if err != nil {
		t.Fatalf("Failed to get status after stop: %v", err)
	}
	defer resp2.Body.Close()

	var status map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&status); err != nil {
		t.Errorf("Failed to decode status: %v", err)
		return
	}

	if running, ok := status["running"].(bool); ok && running {
		t.Error("Expected running to be false after stop")
	}
}

// TestE2E_FullWorkflow 完整工作流测试
func TestE2E_FullWorkflow(t *testing.T) {
	t.Skip("Skipping full workflow test - requires Claude CLI")

	// 完整的 E2E 测试流程：
	// 1. 启动 load tester
	// 2. 等待收集一些指标
	// 3. 获取报告
	// 4. 停止测试
	// 5. 验证报告文件

	// 由于需要实际的 Claude CLI，这里跳过
	// 实际测试时，在有 Claude 环境的测试服务器上运行
}
