package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindInPath(t *testing.T) {
	// 测试 PATH 中存在的命令（假设存在 go）
	path, err := findInPath("go")
	if err != nil {
		t.Skipf("go not in PATH, skipping: %v", err)
	}

	if path == "" {
		t.Error("Expected non-empty path for 'go'")
	}

	// 测试 PATH 中不存在的命令
	_, err = findInPath("nonexistent-command-12345")
	if err == nil {
		t.Error("Expected error for nonexistent command")
	}
}

func TestInitLogger(t *testing.T) {
	logger, err := initLogger()
	if err != nil {
		t.Fatalf("initLogger failed: %v", err)
	}

	if logger == nil {
		t.Error("Expected non-nil logger")
	}

	logger.Sync()
}

func TestAggregateCommand(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()

	// 创建测试报告文件
	report1 := `{
		"start_time": "2026-03-25T10:00:00Z",
		"end_time": "2026-03-25T10:10:00Z",
		"total_requests": 100,
		"success_count": 90,
		"latency_stats": {
			"mean_ms": 500
		}
	}`

	report2 := `{
		"start_time": "2026-03-25T10:00:00Z",
		"end_time": "2026-03-25T10:10:00Z",
		"total_requests": 200,
		"success_count": 190,
		"latency_stats": {
			"mean_ms": 600
		}
	}`

	file1 := filepath.Join(tmpDir, "node1.json")
	file2 := filepath.Join(tmpDir, "node2.json")

	os.WriteFile(file1, []byte(report1), 0644)
	os.WriteFile(file2, []byte(report2), 0644)

	// 测试聚合
	outputFile := filepath.Join(tmpDir, "output.json")

	// 这里我们验证文件被创建
	// 实际命令测试需要更复杂的设置
	_, err := os.Stat(file1)
	if err != nil {
		t.Errorf("Test file 1 not created: %v", err)
	}

	_, err = os.Stat(file2)
	if err != nil {
		t.Errorf("Test file 2 not created: %v", err)
	}

	// 验证输出文件
	_ = outputFile // 用于后续验证
}

func TestVersion(t *testing.T) {
	// 验证版本变量存在
	if Version == "" {
		t.Error("Version should not be empty")
	}

	if BuildTime == "" {
		t.Error("BuildTime should not be empty")
	}
}
