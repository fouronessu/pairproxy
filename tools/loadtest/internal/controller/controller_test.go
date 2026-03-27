package controller

import (
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ClaudePath != "claude" {
		t.Errorf("Expected ClaudePath 'claude', got %s", cfg.ClaudePath)
	}

	if cfg.MaxWorkers != 50 {
		t.Errorf("Expected MaxWorkers 50, got %d", cfg.MaxWorkers)
	}

	if cfg.Mode != "ramp-up" {
		t.Errorf("Expected Mode 'ramp-up', got %s", cfg.Mode)
	}
}

func TestNewController(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultConfig()

	ctrl, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if ctrl == nil {
		t.Fatal("Expected non-nil controller")
	}
}

func TestControllerIsRunning(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultConfig()

	ctrl, _ := New(cfg, logger)

	// 初始状态应为未运行
	if ctrl.IsRunning() {
		t.Error("Expected IsRunning to be false initially")
	}

	// 设置开始时间
	ctrl.startTime = time.Now()
	ctrl.shouldStop = false

	if !ctrl.IsRunning() {
		t.Error("Expected IsRunning to be true after setting startTime")
	}
}

func TestControllerSetters(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultConfig()

	ctrl, _ := New(cfg, logger)

	// 测试 SetMode
	ctrl.SetMode("fixed")
	if ctrl.config.Mode != "fixed" {
		t.Errorf("Expected Mode 'fixed', got %s", ctrl.config.Mode)
	}

	// 测试 SetFixedWorkers
	ctrl.SetFixedWorkers(100)
	if ctrl.config.FixedWorkers != 100 {
		t.Errorf("Expected FixedWorkers 100, got %d", ctrl.config.FixedWorkers)
	}

	// 测试 SetDuration
	ctrl.SetDuration(30 * time.Minute)
	if ctrl.config.Duration != 30*time.Minute {
		t.Errorf("Expected Duration 30m, got %v", ctrl.config.Duration)
	}
}

func TestControllerUpdateConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultConfig()

	ctrl, _ := New(cfg, logger)

	newCfg := &Config{
		Mode:         "spike",
		MaxWorkers:   200,
		FixedWorkers: 100,
	}

	ctrl.UpdateConfig(newCfg)

	if ctrl.config.Mode != "spike" {
		t.Errorf("Expected Mode 'spike', got %s", ctrl.config.Mode)
	}

	if ctrl.config.MaxWorkers != 200 {
		t.Errorf("Expected MaxWorkers 200, got %d", ctrl.config.MaxWorkers)
	}
}

func TestFileExists(t *testing.T) {
	// 测试存在的文件
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	if !fileExists(tmpFile.Name()) {
		t.Error("Expected fileExists to return true for existing file")
	}

	// 测试不存在的文件
	if fileExists("/nonexistent/path/to/file") {
		t.Error("Expected fileExists to return false for nonexistent file")
	}
}
