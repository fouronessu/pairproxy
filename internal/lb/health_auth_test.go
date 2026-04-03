package lb

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestHealthChecker_Anthropic_Auth 验证 Anthropic 请求携带 x-api-key 和 anthropic-version 头
func TestHealthChecker_Anthropic_Auth(t *testing.T) {
	logger := zap.NewNop()

	// Mock server 验证 Anthropic 认证头
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建 balancer 和 health checker
	target := Target{ID: "anthropic-api", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger,
		WithTimeout(1*time.Second),
		WithCredentials(map[string]TargetCredential{
			"anthropic-api": {
				APIKey:   "sk-ant-test-key-12345",
				Provider: "anthropic",
			},
		}),
	)

	// 执行健康检查
	hc.CheckTarget("anthropic-api")
	hc.Wait()

	// 验证请求头
	assert.Equal(t, "sk-ant-test-key-12345", capturedHeaders.Get("x-api-key"))
	assert.Equal(t, "2023-06-01", capturedHeaders.Get("anthropic-version"))
	// 确保没有使用 Bearer 认证
	assert.NotContains(t, capturedHeaders.Get("Authorization"), "Bearer")
}

// TestHealthChecker_OpenAI_Auth 验证 OpenAI/OpenAI-compatible 请求携带 Authorization: Bearer 头
func TestHealthChecker_OpenAI_Auth(t *testing.T) {
	logger := zap.NewNop()

	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := Target{ID: "openai-api", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger,
		WithTimeout(1*time.Second),
		WithCredentials(map[string]TargetCredential{
			"openai-api": {
				APIKey:   "sk-proj-test-key-67890",
				Provider: "openai",
			},
		}),
	)

	hc.CheckTarget("openai-api")
	hc.Wait()

	// 验证 Bearer 认证头
	assert.Equal(t, "Bearer sk-proj-test-key-67890", capturedHeaders.Get("Authorization"))
	// 确保没有使用 Anthropic 的 x-api-key
	assert.Empty(t, capturedHeaders.Get("x-api-key"))
	assert.Empty(t, capturedHeaders.Get("anthropic-version"))
}

// TestHealthChecker_DashScope_Auth 验证阿里百炼 DashScope 使用标准 Bearer 认证
func TestHealthChecker_DashScope_Auth(t *testing.T) {
	logger := zap.NewNop()

	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := Target{ID: "dashscope-api", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger,
		WithTimeout(1*time.Second),
		WithCredentials(map[string]TargetCredential{
			"dashscope-api": {
				APIKey:   "sk-dashscope-test",
				Provider: "openai", // DashScope 兼容 OpenAI
			},
		}),
	)

	hc.CheckTarget("dashscope-api")
	hc.Wait()

	// 验证 Bearer 认证
	assert.Equal(t, "Bearer sk-dashscope-test", capturedHeaders.Get("Authorization"))
}

// TestHealthChecker_NoKey_NoAuthHeader 无 APIKey 时不注入任何认证头（本地 vLLM/sglang 行为不变）
func TestHealthChecker_NoKey_NoAuthHeader(t *testing.T) {
	logger := zap.NewNop()

	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建 target，不配置任何凭证
	target := Target{ID: "local-vllm", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger, WithTimeout(1*time.Second))

	hc.CheckTarget("local-vllm")
	hc.Wait()

	// 验证没有注入任何认证头
	assert.Empty(t, capturedHeaders.Get("Authorization"))
	assert.Empty(t, capturedHeaders.Get("x-api-key"))
	assert.Empty(t, capturedHeaders.Get("anthropic-version"))
}

// TestHealthChecker_UpdateCredentials_Runtime 运行时热更新 credentials 后，下次检查使用新 key
func TestHealthChecker_UpdateCredentials_Runtime(t *testing.T) {
	logger := zap.NewNop()

	callCount := 0
	var lastHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		lastHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := Target{ID: "test-api", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger,
		WithTimeout(1*time.Second),
		WithCredentials(map[string]TargetCredential{
			"test-api": {
				APIKey:   "old-key",
				Provider: "openai",
			},
		}),
	)

	// 第一次检查：使用旧 key
	hc.CheckTarget("test-api")
	hc.Wait()
	assert.Equal(t, "Bearer old-key", lastHeaders.Get("Authorization"))
	assert.Equal(t, 1, callCount)

	// 运行时更新凭证
	hc.UpdateCredentials(map[string]TargetCredential{
		"test-api": {
			APIKey:   "new-key",
			Provider: "openai",
		},
	})

	// 第二次检查：使用新 key
	hc.CheckTarget("test-api")
	hc.Wait()
	assert.Equal(t, "Bearer new-key", lastHeaders.Get("Authorization"))
	assert.Equal(t, 2, callCount)
}

// TestHealthChecker_401_StillTriggersFailure 确认即使有 key，401 也正常触发 recordFailure
func TestHealthChecker_401_StillTriggersFailure(t *testing.T) {
	logger := zap.NewNop()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 key 已注入，但服务返回 401
		assert.NotEmpty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer server.Close()

	target := Target{ID: "test-api", Addr: server.URL, Weight: 1, Healthy: true}
	bal := NewWeightedRandom([]Target{target})
	hc := NewHealthChecker(bal, logger,
		WithTimeout(1*time.Second),
		WithFailThreshold(1), // 单次失败即标记不健康
		WithCredentials(map[string]TargetCredential{
			"test-api": {
				APIKey:   "some-key",
				Provider: "openai",
			},
		}),
	)

	hc.CheckTarget("test-api")
	hc.Wait()

	// 确认节点被标记为不健康（401 仍然是失败）
	targets := bal.Targets()
	found := false
	for _, tgt := range targets {
		if tgt.ID == "test-api" {
			assert.False(t, tgt.Healthy, "401 should still mark node as unhealthy")
			found = true
			break
		}
	}
	assert.True(t, found, "test-api target should exist in balancer")
}
