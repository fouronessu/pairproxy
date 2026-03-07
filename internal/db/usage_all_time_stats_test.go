package db

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// ---------------------------------------------------------------------------
// TestGetUserAllTimeStats — UsageRepo.GetUserAllTimeStats
// ---------------------------------------------------------------------------
//
// 所有测试直接用 gorm 写入 UsageLog，不经过 UsageWriter，
// 确保数据同步入库、查询结果确定（不受异步 flush 时序影响）。

func TestGetUserAllTimeStats_Empty(t *testing.T) {
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, zaptest.NewLogger(t))

	stats, err := repo.GetUserAllTimeStats()
	if err != nil {
		t.Fatalf("GetUserAllTimeStats on empty table: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 stats, got %d", len(stats))
	}
}

func TestGetUserAllTimeStats_SingleUser(t *testing.T) {
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, zaptest.NewLogger(t))

	now := time.Now()
	gormDB.Create(&UsageLog{RequestID: "req-1", UserID: "user-a", Model: "claude-3",
		InputTokens: 100, OutputTokens: 200, CreatedAt: now.Add(-48 * time.Hour)})
	gormDB.Create(&UsageLog{RequestID: "req-2", UserID: "user-a", Model: "claude-3",
		InputTokens: 50, OutputTokens: 75, CreatedAt: now.Add(-24 * time.Hour)})

	stats, err := repo.GetUserAllTimeStats()
	if err != nil {
		t.Fatalf("GetUserAllTimeStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat entry, got %d", len(stats))
	}
	s := stats[0]
	if s.UserID != "user-a" {
		t.Errorf("UserID = %q, want %q", s.UserID, "user-a")
	}
	if s.TotalInput != 150 {
		t.Errorf("TotalInput = %d, want 150", s.TotalInput)
	}
	if s.TotalOutput != 275 {
		t.Errorf("TotalOutput = %d, want 275", s.TotalOutput)
	}
	if s.TotalTokens != 425 {
		t.Errorf("TotalTokens = %d, want 425", s.TotalTokens)
	}
	// 两条记录分属不同日期 → DaysActive = 2
	if s.DaysActive != 2 {
		t.Errorf("DaysActive = %d, want 2", s.DaysActive)
	}
	// FirstUsedAt 应早于 LastUsedAt
	if !s.FirstUsedAt.Before(s.LastUsedAt) {
		t.Errorf("FirstUsedAt (%v) should be before LastUsedAt (%v)", s.FirstUsedAt, s.LastUsedAt)
	}
}

func TestGetUserAllTimeStats_MultipleUsers_OrderedByTotalTokensDesc(t *testing.T) {
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, zaptest.NewLogger(t))

	now := time.Now()
	// user-b 总量更大（3000）
	gormDB.Create(&UsageLog{RequestID: "r1", UserID: "user-b", InputTokens: 1000, OutputTokens: 2000, CreatedAt: now})
	// user-a 总量较小（300）
	gormDB.Create(&UsageLog{RequestID: "r2", UserID: "user-a", InputTokens: 100, OutputTokens: 200, CreatedAt: now})

	stats, err := repo.GetUserAllTimeStats()
	if err != nil {
		t.Fatalf("GetUserAllTimeStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(stats))
	}
	// 第一个应是 user-b（总量 3000 > 300）
	if stats[0].UserID != "user-b" {
		t.Errorf("first entry should be user-b (highest total), got %q", stats[0].UserID)
	}
	if stats[0].TotalTokens != 3000 {
		t.Errorf("user-b TotalTokens = %d, want 3000", stats[0].TotalTokens)
	}
	if stats[1].TotalTokens != 300 {
		t.Errorf("user-a TotalTokens = %d, want 300", stats[1].TotalTokens)
	}
}

func TestGetUserAllTimeStats_DaysActiveCountDistinctDates(t *testing.T) {
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, zaptest.NewLogger(t))

	// 同一天内 3 条记录 → DaysActive 仍应为 1
	day := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	gormDB.Create(&UsageLog{RequestID: "d1", UserID: "u1", InputTokens: 10, CreatedAt: day.Add(1 * time.Hour)})
	gormDB.Create(&UsageLog{RequestID: "d2", UserID: "u1", InputTokens: 10, CreatedAt: day.Add(2 * time.Hour)})
	gormDB.Create(&UsageLog{RequestID: "d3", UserID: "u1", InputTokens: 10, CreatedAt: day.Add(3 * time.Hour)})

	stats, err := repo.GetUserAllTimeStats()
	if err != nil {
		t.Fatalf("GetUserAllTimeStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1, got %d", len(stats))
	}
	if stats[0].DaysActive != 1 {
		t.Errorf("DaysActive = %d, want 1 (same day records should count as 1)", stats[0].DaysActive)
	}
}

func TestGetUserAllTimeStats_MonthsActive(t *testing.T) {
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, zaptest.NewLogger(t))

	// 最早记录距今约 65 天 → MonthsActive ≥ 1
	old := time.Now().AddDate(0, 0, -65)
	gormDB.Create(&UsageLog{RequestID: "m1", UserID: "user-m", InputTokens: 500, CreatedAt: old})
	gormDB.Create(&UsageLog{RequestID: "m2", UserID: "user-m", InputTokens: 100, CreatedAt: time.Now()})

	stats, err := repo.GetUserAllTimeStats()
	if err != nil {
		t.Fatalf("GetUserAllTimeStats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1, got %d", len(stats))
	}
	// 65 天 / 30 ≈ 2 months（CAST AS INTEGER 向下取整）
	if stats[0].MonthsActive < 1 {
		t.Errorf("MonthsActive = %d, want ≥ 1 for ~65 days history", stats[0].MonthsActive)
	}
}

func TestGetUserAllTimeStats_ParseFlexTime(t *testing.T) {
	// 验证 parseFlexTime 能正确处理 SQLite 常见时间格式
	cases := []struct {
		name string
		raw  string
	}{
		{"RFC3339", "2025-03-15T10:30:00Z"},
		{"RFC3339Nano", "2025-03-15T10:30:00.123456789Z"},
		{"SQLite datetime", "2025-03-15 10:30:00"},
		{"SQLite with tz", "2025-03-15T10:30:00+08:00"},
		{"date only", "2025-03-15"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, err := parseFlexTime(c.raw)
			if err != nil {
				t.Errorf("parseFlexTime(%q) error: %v", c.raw, err)
			}
			if parsed.IsZero() {
				t.Errorf("parseFlexTime(%q) returned zero time", c.raw)
			}
		})
	}
}
