package metrics

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewCollector(t *testing.T) {
	logger := zap.NewNop()
	collector := NewCollector(logger)

	if collector == nil {
		t.Fatal("Expected non-nil collector")
	}
}

func TestCollectorRecord(t *testing.T) {
	logger := zap.NewNop()
	collector := NewCollector(logger)

	// 记录一些结果
	for i := 0; i < 10; i++ {
		collector.Record(&Result{
			Timestamp:  time.Now(),
			WorkerID:   i,
			Duration:   time.Duration(100+i*10) * time.Millisecond,
			Success:    i%3 != 0, // 30% 失败率
			OutputSize: 100 + i*5,
		})
	}

	// 获取快照
	snapshot := collector.GetSnapshot(5)

	if snapshot.TotalRequests != 10 {
		t.Errorf("Expected 10 total requests, got %d", snapshot.TotalRequests)
	}

	// 验证成功率计算
	expectedSuccessRate := float64(6) / float64(10) * 100 // 6 成功，4 失败
	if snapshot.SuccessRate != expectedSuccessRate {
		t.Errorf("Expected success rate %.2f, got %.2f", expectedSuccessRate, snapshot.SuccessRate)
	}
}

func TestCalculateLatencyStats(t *testing.T) {
	latencies := []float64{
		100, 200, 300, 400, 500,
		600, 700, 800, 900, 1000,
	}

	stats := calculateLatencyStats(latencies)

	if stats.Min != 100 {
		t.Errorf("Expected min 100, got %f", stats.Min)
	}

	if stats.Max != 1000 {
		t.Errorf("Expected max 1000, got %f", stats.Max)
	}

	if stats.Mean != 550 {
		t.Errorf("Expected mean 550, got %f", stats.Mean)
	}

	if stats.P50 != 550 {
		t.Errorf("Expected P50 550, got %f", stats.P50)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		p        float64
		expected float64
	}{
		{50, 5.5},
		{90, 9.1},
		{95, 9.55},
		{99, 9.91},
	}

	for _, tt := range tests {
		result := percentile(sorted, tt.p)
		// Use epsilon comparison for floating point
		epsilon := 0.0001
		if math.Abs(result-tt.expected) > epsilon {
			t.Errorf("percentile(%v, %f) = %f, expected %f", sorted, tt.p, result, tt.expected)
		}
	}
}

func TestReportSaveToFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_report.json")

	report := &Report{
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(10 * time.Minute),
		Duration:      10 * time.Minute,
		TotalWorkers:  10,
		TotalRequests: 100,
		SuccessCount:  95,
		FailureCount:  5,
		SuccessRate:   95.0,
		LatencyStats: LatencyStats{
			Min:  100,
			Max:  1000,
			Mean: 500,
			P50:  450,
			P90:  900,
			P95:  950,
			P99:  990,
		},
	}

	err := report.SaveToFile(testFile)
	if err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Report file was not created")
	}

	// 读取并验证
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read report file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Report file is empty")
	}
}

func TestAggregator(t *testing.T) {
	agg := NewAggregator()

	// 添加多个报告
	report1 := &Report{
		TotalRequests: 100,
		SuccessCount:  90,
		FailureCount:  10,
		LatencyStats:  LatencyStats{Mean: 500},
	}

	report2 := &Report{
		TotalRequests: 200,
		SuccessCount:  190,
		FailureCount:  10,
		LatencyStats:  LatencyStats{Mean: 600},
	}

	agg.Add(report1)
	agg.Add(report2)

	// 聚合
	result := agg.Aggregate()

	if result.TotalRequests != 300 {
		t.Errorf("Expected 300 total requests, got %d", result.TotalRequests)
	}

	if result.SuccessCount != 280 {
		t.Errorf("Expected 280 success count, got %d", result.SuccessCount)
	}

	// 验证平均延迟
	expectedMean := (500.0 + 600.0) / 2
	if result.LatencyStats.Mean != expectedMean {
		t.Errorf("Expected mean latency %f, got %f", expectedMean, result.LatencyStats.Mean)
	}
}

func TestLoadFromFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建测试报告文件
	report1 := `{
		"start_time": "2026-03-25T10:00:00Z",
		"total_requests": 100,
		"success_count": 90
	}`

	report2 := `{
		"start_time": "2026-03-25T10:00:00Z",
		"total_requests": 200,
		"success_count": 190
	}`

	file1 := filepath.Join(tmpDir, "report1.json")
	file2 := filepath.Join(tmpDir, "report2.json")

	os.WriteFile(file1, []byte(report1), 0644)
	os.WriteFile(file2, []byte(report2), 0644)

	// 加载
	agg, err := LoadFromFiles([]string{file1, file2})
	if err != nil {
		t.Fatalf("LoadFromFiles failed: %v", err)
	}

	result := agg.Aggregate()
	if result.TotalRequests != 300 {
		t.Errorf("Expected 300 total requests, got %d", result.TotalRequests)
	}
}

func TestReporter(t *testing.T) {
	logger := zap.NewNop()
	collector := NewCollector(logger)

	// 添加一些测试数据
	for i := 0; i < 5; i++ {
		collector.Record(&Result{
			Timestamp: time.Now(),
			WorkerID:  i,
			Duration:  100 * time.Millisecond,
			Success:   true,
		})
	}

	reporter := NewReporter(collector, 100*time.Millisecond, logger)

	// 启动 reporter
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go reporter.Start(func() int { return 5 })

	// 等待几个报告周期
	<-ctx.Done()
	reporter.Stop()
}
