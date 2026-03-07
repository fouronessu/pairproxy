package db

// ---------------------------------------------------------------------------
// DeleteBefore 时间边界用例
//
// DeleteBefore(t) 使用严格小于（<），不包含 t 本身。
// 测试重点：
//   1. 严格小于语义（boundary 时刻的记录不被删）
//   2. 只删目标范围，不误删范围外记录
//   3. 返回正确的删除行数
//   4. 空表调用无 error、返回 0
//   5. before 在所有记录之前——什么都不删
//   6. before 在所有记录之后——全部删完
// ---------------------------------------------------------------------------

import (
	"testing"
	"time"
)

func TestDeleteBefore_StrictlyLessThan_BoundaryRecordSurvives(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	boundary := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	// 恰好在 boundary 时刻的记录——不应被删除
	insertLog(t, repo, "at-boundary", "u1", 10, 10, boundary)
	// boundary 之前 1ns——应被删除
	insertLog(t, repo, "just-before", "u1", 10, 10, boundary.Add(-time.Nanosecond))
	// boundary 之后——应保留
	insertLog(t, repo, "after", "u1", 10, 10, boundary.Add(time.Second))

	deleted, err := repo.DeleteBefore(boundary)
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted %d rows, want 1 (only the one strictly before boundary)", deleted)
	}

	// 验证数据库中剩余 2 条
	var remaining []UsageLog
	repo.db.Find(&remaining)
	if len(remaining) != 2 {
		t.Errorf("remaining %d rows, want 2", len(remaining))
	}
	// at-boundary 应仍在
	for _, r := range remaining {
		if r.RequestID == "just-before" {
			t.Errorf("record 'just-before' should have been deleted")
		}
	}
}

func TestDeleteBefore_DeletesOnlyBeforeRange(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	cutoff := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

	// 应被删除的（旧记录）
	for i := 0; i < 5; i++ {
		insertLog(t, repo,
			"old-"+padInt(i), "user-del",
			10, 10,
			cutoff.AddDate(0, 0, -(i+1)), // cutoff 前 1~5 天
		)
	}
	// 应保留的（新记录）
	for i := 0; i < 3; i++ {
		insertLog(t, repo,
			"new-"+padInt(i), "user-del",
			10, 10,
			cutoff.AddDate(0, 0, i+1), // cutoff 后 1~3 天
		)
	}
	// 恰好在 cutoff（应保留，< 不含等于）
	insertLog(t, repo, "exactly-cutoff", "user-del", 10, 10, cutoff)

	deleted, err := repo.DeleteBefore(cutoff)
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 5 {
		t.Errorf("deleted %d, want 5 (only old records)", deleted)
	}

	var remaining []UsageLog
	repo.db.Find(&remaining)
	// 3 条新记录 + 1 条 exactly-cutoff
	if len(remaining) != 4 {
		t.Errorf("remaining %d rows, want 4", len(remaining))
	}
}

func TestDeleteBefore_ReturnsCorrectCount(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	cutoff := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	// 插入恰好 7 条旧记录
	for i := 0; i < 7; i++ {
		insertLog(t, repo,
			"cnt-"+padInt(i), "user-cnt",
			1, 1,
			cutoff.Add(-time.Duration(i+1)*time.Hour),
		)
	}

	deleted, err := repo.DeleteBefore(cutoff)
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 7 {
		t.Errorf("deleted %d, want 7", deleted)
	}
}

func TestDeleteBefore_EmptyTable_ReturnsZero(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	deleted, err := repo.DeleteBefore(time.Now())
	if err != nil {
		t.Fatalf("DeleteBefore on empty table: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0 on empty table", deleted)
	}
}

func TestDeleteBefore_BeforeAllRecords_NothingDeleted(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	base := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		insertLog(t, repo, "nd-"+padInt(i), "user-nd", 10, 10, base.Add(time.Duration(i)*time.Hour))
	}

	// cutoff 在所有记录之前
	deleted, err := repo.DeleteBefore(base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0 (cutoff before all records)", deleted)
	}

	var remaining []UsageLog
	repo.db.Find(&remaining)
	if len(remaining) != 3 {
		t.Errorf("remaining %d, want 3 (none should be deleted)", len(remaining))
	}
}

func TestDeleteBefore_AfterAllRecords_AllDeleted(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	base := time.Date(2025, 5, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		insertLog(t, repo, "all-"+padInt(i), "user-all", 10, 10, base.Add(time.Duration(i)*time.Hour))
	}

	// cutoff 在所有记录之后
	deleted, err := repo.DeleteBefore(base.Add(10 * time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 4 {
		t.Errorf("deleted %d, want 4 (all records)", deleted)
	}

	var remaining []UsageLog
	repo.db.Find(&remaining)
	if len(remaining) != 0 {
		t.Errorf("remaining %d, want 0 (all deleted)", len(remaining))
	}
}

func TestDeleteBefore_Idempotent(t *testing.T) {
	repo, cleanup := openTestRepoForExport(t)
	defer cleanup()

	cutoff := time.Date(2025, 5, 3, 12, 0, 0, 0, time.UTC)
	insertLog(t, repo, "idem-1", "user-idem", 10, 10, cutoff.Add(-time.Hour))

	// 第一次删除
	n1, err := repo.DeleteBefore(cutoff)
	if err != nil {
		t.Fatalf("first DeleteBefore: %v", err)
	}
	if n1 != 1 {
		t.Errorf("first call deleted %d, want 1", n1)
	}

	// 第二次——什么都没了，应返回 0 且无 error
	n2, err := repo.DeleteBefore(cutoff)
	if err != nil {
		t.Fatalf("second DeleteBefore: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second call deleted %d, want 0 (idempotent)", n2)
	}
}
