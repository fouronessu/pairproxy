// +build integration

// Package e2e_test contains integration tests using real mock processes.
//
// 这些测试使用独立的 mockagent 和 mockllm 进程，模拟真实的部署环境。
// 与 httptest 测试的区别：
//   - httptest: 单进程，快速，适合 CI/CD
//   - 进程测试: 多进程，真实，适合手动验证和压力测试
//
// 运行方式：
//   go test -v -tags=integration ./test/e2e/
package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/db"
)

// TestFullChainWithMockProcesses tests the complete chain using real processes:
// mockagent → sproxy → mockllm
func TestFullChainWithMockProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// Find free ports
	mockllmPort := findFreePort(t)
	sproxyPort := findFreePort(t)

	// Setup database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	gormDB, err := db.Open(logger, dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	// Create test user
	userRepo := db.NewUserRepo(gormDB, logger)
	userRepo.Create(&db.User{ID: "test-user", Username: "testuser", IsActive: true})

	// Generate JWT token
	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	token, err := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "test-user",
		Username: "testuser",
		Role:     "user",
	}, time.Hour)
	if err != nil {
		t.Fatalf("Sign token: %v", err)
	}

	// Start mockllm process
	mockllmCmd := startMockLLM(t, mockllmPort)
	defer mockllmCmd.Process.Kill()

	// Wait for mockllm to be ready
	waitForPort(t, mockllmPort, 5*time.Second)

	// Start sproxy process
	sproxyCmd := startSProxy(t, sproxyPort, mockllmPort, dbPath, token)
	defer sproxyCmd.Process.Kill()

	// Wait for sproxy to be ready
	waitForPort(t, sproxyPort, 5*time.Second)

	// Test 1: Send a simple request
	t.Run("simple_request", func(t *testing.T) {
		content := "Hello, World!"
		received := sendRequest(t, sproxyPort, token, content, false)
		if received != content {
			t.Errorf("received = %q, want %q", received, content)
		}
	})

	// Test 2: Send a streaming request
	t.Run("streaming_request", func(t *testing.T) {
		content := "Streaming test message"
		received := sendRequest(t, sproxyPort, token, content, true)
		if received != content {
			t.Errorf("received = %q, want %q", received, content)
		}
	})

	// Test 3: Multiple concurrent requests
	t.Run("concurrent_requests", func(t *testing.T) {
		const numRequests = 10
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(id int) {
				content := fmt.Sprintf("Message %d", id)
				received := sendRequest(t, sproxyPort, token, content, false)
				if received != content {
					results <- fmt.Errorf("request %d: received = %q, want %q", id, received, content)
				} else {
					results <- nil
				}
			}(i)
		}

		for i := 0; i < numRequests; i++ {
			if err := <-results; err != nil {
				t.Error(err)
			}
		}
	})

	// Verify usage was recorded in database
	t.Run("verify_usage_recorded", func(t *testing.T) {
		time.Sleep(2 * time.Second) // Wait for async writes

		usageRepo := db.NewUsageRepo(gormDB, logger)
		now := time.Now()
		from := now.Add(-1 * time.Hour)
		to := now.Add(1 * time.Hour)

		input, output, err := usageRepo.SumTokens("test-user", from, to)
		if err != nil {
			t.Fatalf("SumTokens: %v", err)
		}

		totalTokens := input + output
		if totalTokens == 0 {
			t.Error("expected usage to be recorded, got 0 tokens")
		}

		t.Logf("Total tokens used: %d (input: %d, output: %d)", totalTokens, input, output)
	})
}

// findFreePort finds an available TCP port
func findFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// waitForPort waits for a port to be listening
func waitForPort(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d not ready after %v", port, timeout)
}

// startMockLLM starts the mockllm process
func startMockLLM(t *testing.T, port int) *exec.Cmd {
	t.Helper()

	// Build mockllm if not exists
	mockllmPath := filepath.Join(".", "mockllm.exe")
	if _, err := os.Stat(mockllmPath); os.IsNotExist(err) {
		buildCmd := exec.Command("go", "build", "-o", mockllmPath, "./cmd/mockllm")
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to build mockllm: %v\n%s", err, output)
		}
	}

	cmd := exec.Command(mockllmPath, "--addr", fmt.Sprintf(":%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start mockllm: %v", err)
	}

	t.Logf("Started mockllm on port %d (PID: %d)", port, cmd.Process.Pid)
	return cmd
}

// startSProxy starts the sproxy process
func startSProxy(t *testing.T, sproxyPort, mockllmPort int, dbPath, token string) *exec.Cmd {
	t.Helper()

	// Create temporary config file
	configPath := filepath.Join(t.TempDir(), "sproxy.yaml")
	config := fmt.Sprintf(`
server:
  addr: ":%d"

db:
  path: "%s"

jwt:
  secret: "test-secret"

llm:
  targets:
    - url: "http://127.0.0.1:%d"
      api_key: "mock-key"
      provider: "anthropic"
`, sproxyPort, dbPath, mockllmPort)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Build sproxy if not exists
	sproxyPath := filepath.Join(".", "sproxy.exe")
	if _, err := os.Stat(sproxyPath); os.IsNotExist(err) {
		buildCmd := exec.Command("go", "build", "-o", sproxyPath, "./cmd/sproxy")
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to build sproxy: %v\n%s", err, output)
		}
	}

	cmd := exec.Command(sproxyPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sproxy: %v", err)
	}

	t.Logf("Started sproxy on port %d (PID: %d)", sproxyPort, cmd.Process.Pid)
	return cmd
}

// sendRequest sends a request to sproxy and returns the response content
func sendRequest(t *testing.T, port int, token, content string, stream bool) string {
	t.Helper()

	reqBody := map[string]interface{}{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": content},
		},
		"stream": stream,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body: %s", resp.StatusCode, body)
	}

	if stream {
		return readStreamingResponse(t, resp.Body)
	}
	return readNonStreamingResponse(t, resp.Body)
}

// readStreamingResponse reads SSE streaming response
func readStreamingResponse(t *testing.T, r io.Reader) string {
	t.Helper()
	var result strings.Builder
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if delta, ok := event["delta"].(map[string]interface{}); ok {
			if text, ok := delta["text"].(string); ok {
				result.WriteString(text)
			}
		}
	}

	return result.String()
}

// readNonStreamingResponse reads JSON response
func readNonStreamingResponse(t *testing.T, r io.Reader) string {
	t.Helper()
	var resp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Content) == 0 {
		return ""
	}
	return resp.Content[0].Text
}
