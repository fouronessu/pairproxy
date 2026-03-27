package worker

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/l17728/pairproxy/tools/loadtest/internal/metrics"
)

func TestNewWorker(t *testing.T) {
	logger := zap.NewNop()
	promptsCh := make(chan string, 10)
	resultsCh := make(chan *metrics.Result, 10)

	cfg := Config{
		WorkerID:     1,
		ClaudePath:   "echo", // 使用 echo 作为 mock
		Timeout:      5 * time.Second,
		ThinkTimeMin: 0,
		ThinkTimeMax: 0,
	}

	w := New(cfg, promptsCh, resultsCh, logger)
	if w == nil {
		t.Fatal("Expected non-nil worker")
	}

	if w.id != 1 {
		t.Errorf("Expected worker id 1, got %d", w.id)
	}
}

func TestWorkerStartStop(t *testing.T) {
	logger := zap.NewNop()
	promptsCh := make(chan string, 10)
	resultsCh := make(chan *metrics.Result, 10)

	cfg := Config{
		WorkerID:     1,
		ClaudePath:   "echo",
		Timeout:      5 * time.Second,
		ThinkTimeMin: 0,
		ThinkTimeMax: 0,
	}

	w := New(cfg, promptsCh, resultsCh, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 worker
	go w.Start(ctx)

	// 等待启动
	time.Sleep(100 * time.Millisecond)

	if !w.IsRunning() {
		t.Error("Expected worker to be running")
	}

	// 发送 prompt
	go func() {
		promptsCh <- "test prompt"
	}()

	// 等待结果
	select {
	case result := <-resultsCh:
		if result == nil {
			t.Error("Expected non-nil result")
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for result")
	}

	// 停止
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestWorkerGetStats(t *testing.T) {
	logger := zap.NewNop()
	promptsCh := make(chan string, 10)
	resultsCh := make(chan *metrics.Result, 10)

	cfg := Config{
		WorkerID:     1,
		ClaudePath:   "echo",
		Timeout:      5 * time.Second,
		ThinkTimeMin: 0,
		ThinkTimeMax: 0,
	}

	w := New(cfg, promptsCh, resultsCh, logger)

	// 初始统计应为 0
	total, success, failure := w.GetStats()
	if total != 0 || success != 0 || failure != 0 {
		t.Errorf("Expected all zeros, got total=%d, success=%d, failure=%d", total, success, failure)
	}
}

func TestPool(t *testing.T) {
	logger := zap.NewNop()
	promptsCh := make(chan string, 10)
	resultsCh := make(chan *metrics.Result, 10)

	cfg := Config{
		ClaudePath:   "echo",
		Timeout:      5 * time.Second,
		ThinkTimeMin: 0,
		ThinkTimeMax: 0,
	}

	// 创建 pool
	pool := NewPool(3, cfg, promptsCh, resultsCh, logger)
	if pool == nil {
		t.Fatal("Expected non-nil pool")
	}

	// 启动 pool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// 验证统计
	workers, total, _, _ := pool.GetStats()
	if workers != 3 {
		t.Errorf("Expected 3 workers, got %d", workers)
	}
	if total != 0 {
		t.Errorf("Expected 0 total requests, got %d", total)
	}

	// 停止
	pool.Stop()
}

func TestPoolScale(t *testing.T) {
	logger := zap.NewNop()
	promptsCh := make(chan string, 10)
	resultsCh := make(chan *metrics.Result, 10)

	cfg := Config{
		ClaudePath:   "echo",
		Timeout:      5 * time.Second,
		ThinkTimeMin: 0,
		ThinkTimeMax: 0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := NewPool(2, cfg, promptsCh, resultsCh, logger)
	pool.Start(ctx)

	// 扩容到 5
	pool.Scale(ctx, 5, promptsCh, resultsCh, logger)
	time.Sleep(100 * time.Millisecond)

	workers, _, _, _ := pool.GetStats()
	if workers != 5 {
		t.Errorf("Expected 5 workers after scale up, got %d", workers)
	}

	// 缩容到 3
	pool.Scale(ctx, 3, promptsCh, resultsCh, logger)
	time.Sleep(100 * time.Millisecond)

	workers, _, _, _ = pool.GetStats()
	if workers != 3 {
		t.Errorf("Expected 3 workers after scale down, got %d", workers)
	}

	pool.Stop()
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is a ..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, expected %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestRandNorm(t *testing.T) {
	// 测试 randNorm 返回合理的值
	for i := 0; i < 100; i++ {
		val := randNorm()
		// 大部分值应在 -3 到 3 之间
		if val < -5 || val > 5 {
			t.Logf("randNorm returned outlier: %f", val)
		}
	}
}
