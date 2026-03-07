package db

// ---------------------------------------------------------------------------
// ExportLogs 分页边界用例
//
// 已有测试（export_test.go）覆盖了：空表、全量在范围内、超过 2 个 pageSize、
// 回调中断、时间过滤、升序排列。
//
// 本文件专门补充"最后一批恰好等于 pageSize"这一边界场景，
// 以及"恰好 1 条""pageSize 整倍数""偏差 ±1"等精确边界。
// ---------------------------------------------------------------------------

import (
	"testing"
	"time"
)

// TestExportLogs_ExactlyOnePageSize 记录数 == pageSize（500）时，
// 循环应恰好执行两次查询：第一次取满 500 条，第二次取到 0 条后退出。
// 如果逻辑写成 len(batch) <= pageSize 则会提前结束，漏掉记录。
func TestExportLogs_ExactlyOnePageSize(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	const pageSize = 500
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < pageSize; i++ {
		insertLog(t, repo,
			"exact-"+padInt(i), "user-exact",
			i+1, i+1,
			base.Add(time.Duration(i)*time.Second),
		)
	}

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour * 24)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if count != pageSize {
		t.Errorf("exported %d rows, want %d (exactly one full page)", count, pageSize)
	}
}

// TestExportLogs_PageSizePlusOne pageSize+1 条：第一页满，第二页只有 1 条。
// 验证分页不丢最后那一条。
func TestExportLogs_PageSizePlusOne(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	const total = 501
	base := time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		insertLog(t, repo,
			"p1-"+padInt(i), "user-p1",
			10, 10,
			base.Add(time.Duration(i)*time.Second),
		)
	}

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour * 24)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if count != total {
		t.Errorf("exported %d rows, want %d (pageSize+1)", count, total)
	}
}

// TestExportLogs_PageSizeMinusOne pageSize-1 条：只有不足一页，
// 一次查询后 len(batch) < pageSize，应直接 break，不做无效的第二次查询。
func TestExportLogs_PageSizeMinusOne(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	const total = 499
	base := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		insertLog(t, repo,
			"pm1-"+padInt(i), "user-pm1",
			5, 5,
			base.Add(time.Duration(i)*time.Second),
		)
	}

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour * 24)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if count != total {
		t.Errorf("exported %d rows, want %d (pageSize-1)", count, total)
	}
}

// TestExportLogs_TwoExactPages 恰好 pageSize*2 条：
// 第一页满→继续，第二页满→继续，第三次查到 0 条→退出。
func TestExportLogs_TwoExactPages(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	const total = 1000 // 500 × 2
	base := time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		insertLog(t, repo,
			"2p-"+padInt(i), "user-2p",
			1, 1,
			base.Add(time.Duration(i)*time.Second),
		)
	}

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour * 24)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if count != total {
		t.Errorf("exported %d rows, want %d (two exact pages)", count, total)
	}
}

// TestExportLogs_SingleRecord 只有 1 条记录时，一次查询即完成，不应死循环。
func TestExportLogs_SingleRecord(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	base := time.Date(2025, 6, 5, 12, 0, 0, 0, time.UTC)
	insertLog(t, repo, "single-1", "user-single", 42, 58, base)

	from := base.Add(-time.Minute)
	to := base.Add(time.Minute)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if count != 1 {
		t.Errorf("exported %d rows, want 1", count)
	}
}

// TestExportLogs_TimeFilterInclusive from/to 端点均应包含在范围内（>=、<=）。
func TestExportLogs_TimeFilterInclusive(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	exact := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)

	// 恰好在 from 端点
	insertLog(t, repo, "at-from", "user-inc", 10, 10, exact)
	// 恰好在 to 端点
	insertLog(t, repo, "at-to", "user-inc", 10, 10, exact.Add(2*time.Hour))
	// 超出范围（晚 1ns）
	insertLog(t, repo, "after-to", "user-inc", 10, 10, exact.Add(2*time.Hour+time.Nanosecond))
	// 超出范围（早 1ns）
	insertLog(t, repo, "before-from", "user-inc", 10, 10, exact.Add(-time.Nanosecond))

	from := exact
	to := exact.Add(2 * time.Hour)

	count := 0
	if err := repo.ExportLogs(from, to, func(_ UsageLog) error { count++; return nil }); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	// 只有 at-from 和 at-to 两条在 [from, to] 范围内
	if count != 2 {
		t.Errorf("exported %d rows, want 2 (inclusive boundary)", count)
	}
}

// padInt 将整数格式化为 4 位对齐字符串，用于生成可排序的唯一 RequestID。
func padInt(n int) string {
	s := "0000" + intToStr(n)
	return s[len(s)-4:]
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
