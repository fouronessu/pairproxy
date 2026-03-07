package db

// ---------------------------------------------------------------------------
// UsageWriter 幂等性用例（重复 RequestID）
//
// writeBatch 使用 OnConflict{DoNothing: true}（即 INSERT OR IGNORE）。
// 测试验证：
//   1. 相同 RequestID 写两次，数据库只保留 1 条（无重复）
//   2. 重复写不覆盖原有字段（原始 token 数不变）
//   3. 不同 RequestID 均正常写入（正路径不受影响）
//   4. 全部重复的 batch —— 成功返回，inserted=0，无 error
//   5. 混合 batch（部分重复）—— 只插入新的，跳过重复的
//   6. 通过 UsageWriter.Record 触发真实写入（集成路径）
// ---------------------------------------------------------------------------

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// writeBatchDirect 绕过 channel，直接调用 writeBatch，
// 用于精确控制写入时序，不依赖异步 flush。
func writeBatchDirect(t *testing.T, w *UsageWriter, records []UsageRecord) {
	t.Helper()
	w.writeBatch(records)
}

func newTestWriter(t *testing.T) (*UsageWriter, *UsageRepo) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, logger)
	writer := NewUsageWriter(gormDB, logger, 200, time.Hour) // 超长间隔，测试中不自动 flush
	return writer, repo
}

// TestIdempotency_DuplicateRequestID 同一 RequestID 写两次，DB 中只保留 1 条。
func TestIdempotency_DuplicateRequestID(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	first := UsageRecord{RequestID: "dup-req-1", UserID: "u1", InputTokens: 100, OutputTokens: 200, CreatedAt: now}
	writeBatchDirect(t, writer, []UsageRecord{first})

	// 完全相同的 RequestID 再写一次
	writeBatchDirect(t, writer, []UsageRecord{first})

	var logs []UsageLog
	repo.db.Where("request_id = ?", "dup-req-1").Find(&logs)
	if len(logs) != 1 {
		t.Errorf("got %d rows for RequestID=dup-req-1, want 1 (deduplication)", len(logs))
	}
}

// TestIdempotency_NoOverwrite 重复写入不覆盖原有字段（INSERT OR IGNORE 语义）。
func TestIdempotency_NoOverwrite(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	original := UsageRecord{
		RequestID:    "noover-1",
		UserID:       "u2",
		InputTokens:  300,
		OutputTokens: 400,
		CreatedAt:    now,
	}
	writeBatchDirect(t, writer, []UsageRecord{original})

	// 用相同 RequestID 但不同 token 数再写一次
	modified := original
	modified.InputTokens = 9999
	modified.OutputTokens = 9999
	writeBatchDirect(t, writer, []UsageRecord{modified})

	var log UsageLog
	repo.db.Where("request_id = ?", "noover-1").First(&log)
	if log.InputTokens != 300 {
		t.Errorf("InputTokens = %d after duplicate write, want 300 (original should survive)", log.InputTokens)
	}
	if log.OutputTokens != 400 {
		t.Errorf("OutputTokens = %d after duplicate write, want 400 (original should survive)", log.OutputTokens)
	}
}

// TestIdempotency_UniqueIDsAllInserted 不同 RequestID 均正常写入（正路径不受影响）。
func TestIdempotency_UniqueIDsAllInserted(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	batch := make([]UsageRecord, 5)
	for i := range batch {
		batch[i] = UsageRecord{
			RequestID:    fmt.Sprintf("unique-%d", i),
			UserID:       "u3",
			InputTokens:  i + 1,
			OutputTokens: i + 1,
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
		}
	}
	writeBatchDirect(t, writer, batch)

	var count int64
	repo.db.Model(&UsageLog{}).Where("user_id = ?", "u3").Count(&count)
	if count != 5 {
		t.Errorf("inserted %d rows, want 5 (all unique IDs)", count)
	}
}

// TestIdempotency_AllDuplicates_NoError 全部重复的 batch——不报 error，inserted=0。
func TestIdempotency_AllDuplicates_NoError(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	r := UsageRecord{RequestID: "alldup-1", UserID: "u4", InputTokens: 50, OutputTokens: 50, CreatedAt: now}

	// 第一次插入
	writeBatchDirect(t, writer, []UsageRecord{r})
	// 全部重复的 batch，应静默跳过，不 panic，不报 error
	writeBatchDirect(t, writer, []UsageRecord{r, r, r})

	var count int64
	repo.db.Model(&UsageLog{}).Where("request_id = ?", "alldup-1").Count(&count)
	if count != 1 {
		t.Errorf("got %d rows, want 1 (all duplicates should be ignored)", count)
	}
}

// TestIdempotency_MixedBatch 部分重复的 batch——只插入新的，跳过重复的。
func TestIdempotency_MixedBatch(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	// 预先写入 2 条
	existing := []UsageRecord{
		{RequestID: "mix-exist-1", UserID: "u5", InputTokens: 10, CreatedAt: now},
		{RequestID: "mix-exist-2", UserID: "u5", InputTokens: 20, CreatedAt: now.Add(time.Second)},
	}
	writeBatchDirect(t, writer, existing)

	// 混合 batch：2 条重复 + 3 条新的
	mixed := []UsageRecord{
		{RequestID: "mix-exist-1", UserID: "u5", InputTokens: 10, CreatedAt: now},    // 重复
		{RequestID: "mix-exist-2", UserID: "u5", InputTokens: 20, CreatedAt: now},    // 重复
		{RequestID: "mix-new-1", UserID: "u5", InputTokens: 30, CreatedAt: now},      // 新
		{RequestID: "mix-new-2", UserID: "u5", InputTokens: 40, CreatedAt: now},      // 新
		{RequestID: "mix-new-3", UserID: "u5", InputTokens: 50, CreatedAt: now},      // 新
	}
	writeBatchDirect(t, writer, mixed)

	var count int64
	repo.db.Model(&UsageLog{}).Where("user_id = ?", "u5").Count(&count)
	// 2 条原有 + 3 条新增 = 5 条
	if count != 5 {
		t.Errorf("got %d rows, want 5 (2 existing + 3 new)", count)
	}
}

// TestIdempotency_ViaRecordAndFlush 通过 UsageWriter.Record 触发真实写入路径的幂等验证。
// 使用 cancel + Wait() 确保所有记录都被 goroutine 处理完，不依赖 Flush 时序。
func TestIdempotency_ViaRecordAndFlush(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB := openTestDB(t)
	repo := NewUsageRepo(gormDB, logger)
	writer := NewUsageWriter(gormDB, logger, 200, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	writer.Start(ctx)

	now := time.Now()
	r := UsageRecord{
		RequestID:    "via-record-1",
		UserID:       "u6",
		InputTokens:  77,
		OutputTokens: 88,
		CreatedAt:    now,
	}

	// 写入同一条两次
	writer.Record(r)
	writer.Record(r)

	// 停止 writer：cancel 使 goroutine 进入 drain 逻辑，Wait 等待其写完
	cancel()
	writer.Wait()

	var count int64
	repo.db.Model(&UsageLog{}).Where("request_id = ?", "via-record-1").Count(&count)
	if count != 1 {
		t.Errorf("got %d rows via Record+Wait, want 1 (idempotent)", count)
	}
}

// TestIdempotency_CrossBatchDuplicate 跨两次独立的 writeBatch 调用的去重。
// 模拟网络重传：同一请求在不同批次中被提交两次。
func TestIdempotency_CrossBatchDuplicate(t *testing.T) {
	writer, repo := newTestWriter(t)
	now := time.Now()

	r := UsageRecord{RequestID: "cross-1", UserID: "u7", InputTokens: 111, OutputTokens: 222, CreatedAt: now}

	// 第一批
	writeBatchDirect(t, writer, []UsageRecord{r})
	// 模拟网络重传，第二批里又出现了同一条
	writeBatchDirect(t, writer, []UsageRecord{
		{RequestID: "cross-2", UserID: "u7", InputTokens: 10, CreatedAt: now},
		r, // 重复
	})

	var count int64
	repo.db.Model(&UsageLog{}).Where("user_id = ?", "u7").Count(&count)
	if count != 2 {
		t.Errorf("got %d rows, want 2 (cross-1 and cross-2)", count)
	}

	var log UsageLog
	repo.db.Where("request_id = ?", "cross-1").First(&log)
	if log.InputTokens != 111 {
		t.Errorf("cross-1 InputTokens = %d, want 111 (not overwritten)", log.InputTokens)
	}
}
