package track

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTestTracker 创建临时目录的 Tracker（测试辅助）。
func openTestTracker(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	tracker, err := New(filepath.Join(dir, "track"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tracker
}

// ---------------------------------------------------------------------------
// Tracker 基础行为
// ---------------------------------------------------------------------------

func TestTracker_EnableDisable_IsTracked(t *testing.T) {
	tr := openTestTracker(t)

	// 初始未跟踪
	if tr.IsTracked("alice") {
		t.Error("alice should not be tracked initially")
	}

	// Enable
	if err := tr.Enable("alice"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !tr.IsTracked("alice") {
		t.Error("alice should be tracked after Enable")
	}

	// Disable
	if err := tr.Disable("alice"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if tr.IsTracked("alice") {
		t.Error("alice should not be tracked after Disable")
	}
}

func TestTracker_Enable_Idempotent(t *testing.T) {
	tr := openTestTracker(t)

	for i := 0; i < 3; i++ {
		if err := tr.Enable("bob"); err != nil {
			t.Fatalf("Enable iteration %d: %v", i, err)
		}
	}
	if !tr.IsTracked("bob") {
		t.Error("bob should be tracked after multiple Enable calls")
	}
}

func TestTracker_Disable_NonExistentIsOK(t *testing.T) {
	tr := openTestTracker(t)
	// 未 Enable 直接 Disable 不应报错
	if err := tr.Disable("ghost"); err != nil {
		t.Errorf("Disable of non-existent user should not error: %v", err)
	}
}

func TestTracker_ListTracked(t *testing.T) {
	tr := openTestTracker(t)

	users := []string{"alice", "bob", "carol"}
	for _, u := range users {
		if err := tr.Enable(u); err != nil {
			t.Fatalf("Enable %s: %v", u, err)
		}
	}

	got, err := tr.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked: %v", err)
	}
	if len(got) != len(users) {
		t.Errorf("ListTracked: got %d users, want %d: %v", len(got), len(users), got)
	}

	// 禁用一个，重新 list
	if err := tr.Disable("bob"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	got2, err := tr.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked after disable: %v", err)
	}
	if len(got2) != 2 {
		t.Errorf("after disabling bob: got %d users, want 2: %v", len(got2), got2)
	}
	for _, u := range got2 {
		if u == "bob" {
			t.Error("bob should not appear in ListTracked after Disable")
		}
	}
}

func TestTracker_ListTracked_EmptyWhenNone(t *testing.T) {
	tr := openTestTracker(t)
	got, err := tr.ListTracked()
	if err != nil {
		t.Fatalf("ListTracked on empty tracker: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestTracker_ValidateUsername_RejectsInvalid(t *testing.T) {
	tr := openTestTracker(t)

	cases := []string{"", "../evil", "a/b", "a\\b", "a..b"}
	for _, u := range cases {
		if err := tr.Enable(u); err == nil {
			t.Errorf("Enable(%q) should have errored (path traversal risk)", u)
		}
	}
}

func TestTracker_ValidateUsername_AcceptsNormal(t *testing.T) {
	tr := openTestTracker(t)
	cases := []string{"alice", "bob123", "user.name", "user-name", "USER"}
	for _, u := range cases {
		if err := tr.Enable(u); err != nil {
			t.Errorf("Enable(%q) should not error: %v", u, err)
		}
	}
}

// ---------------------------------------------------------------------------
// CaptureSession：消息提取
// ---------------------------------------------------------------------------

func TestCaptureSession_ExtractMessages_Anthropic(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there!"},
			{"role": "user", "content": "How are you?"}
		]
	}`)

	tr := openTestTracker(t)
	_ = tr.Enable("alice")
	cs := NewCaptureSession(tr.UserConvDir("alice"), "req-1", "alice", body, "anthropic")

	if len(cs.record.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(cs.record.Messages))
	}
	if cs.record.Messages[0].Role != "user" || cs.record.Messages[0].Content != "Hello" {
		t.Errorf("message 0: got %+v", cs.record.Messages[0])
	}
	if cs.record.Model != "claude-3-opus" {
		t.Errorf("model: got %q, want claude-3-opus", cs.record.Model)
	}
}

func TestCaptureSession_ExtractMessages_ContentBlocks(t *testing.T) {
	// Anthropic 格式：content 为 block 数组
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "Hello"},
				{"type": "image", "source": {}}
			]}
		]
	}`)

	cs := NewCaptureSession(t.TempDir(), "req-2", "alice", body, "anthropic")
	if len(cs.record.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cs.record.Messages))
	}
	// text block 提取
	if cs.record.Messages[0].Content != "Hello" {
		t.Errorf("content block extraction: got %q, want %q", cs.record.Messages[0].Content, "Hello")
	}
}

// ---------------------------------------------------------------------------
// CaptureSession：非流式响应
// ---------------------------------------------------------------------------

func TestCaptureSession_NonStreaming_Anthropic(t *testing.T) {
	responseBody := []byte(`{
		"type": "message",
		"content": [{"type": "text", "text": "I'm Claude!"}],
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)

	dir := t.TempDir()
	cs := NewCaptureSession(dir, "req-ns-1", "alice", []byte("{}"), "anthropic")
	cs.SetNonStreamingResponse(responseBody)
	cs.Flush()

	// 读取写入的文件
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	var rec ConversationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse record: %v", err)
	}
	if rec.Response != "I'm Claude!" {
		t.Errorf("response: got %q, want %q", rec.Response, "I'm Claude!")
	}
	if rec.InputTokens != 10 || rec.OutputTokens != 5 {
		t.Errorf("tokens: got in=%d out=%d, want in=10 out=5", rec.InputTokens, rec.OutputTokens)
	}
}

func TestCaptureSession_NonStreaming_OpenAI(t *testing.T) {
	responseBody := []byte(`{
		"choices": [{"message": {"content": "OpenAI says hello"}}],
		"usage": {"prompt_tokens": 8, "completion_tokens": 4}
	}`)

	dir := t.TempDir()
	cs := NewCaptureSession(dir, "req-ns-2", "bob", []byte("{}"), "openai")
	cs.SetNonStreamingResponse(responseBody)
	cs.Flush()

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	var rec ConversationRecord
	json.Unmarshal(data, &rec) //nolint:errcheck
	if rec.Response != "OpenAI says hello" {
		t.Errorf("response: got %q", rec.Response)
	}
	if rec.InputTokens != 8 || rec.OutputTokens != 4 {
		t.Errorf("tokens: got in=%d out=%d", rec.InputTokens, rec.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// CaptureSession：流式响应（SSE chunk 累积）
// ---------------------------------------------------------------------------

func TestCaptureSession_Streaming_Anthropic(t *testing.T) {
	dir := t.TempDir()
	cs := NewCaptureSession(dir, "req-sse-1", "alice", []byte("{}"), "anthropic")

	// 模拟多个 SSE chunks
	chunks := []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\", world!\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}

	for _, chunk := range chunks {
		cs.FeedChunk([]byte(chunk))
	}

	// message_stop 应触发自动 Flush
	// 等待写入（FeedChunk 是同步的，不需要等待）
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file after stream end, got %d", len(entries))
	}

	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	var rec ConversationRecord
	json.Unmarshal(data, &rec) //nolint:errcheck
	if rec.Response != "Hello, world!" {
		t.Errorf("streaming response: got %q, want %q", rec.Response, "Hello, world!")
	}
}

func TestCaptureSession_Streaming_OpenAI(t *testing.T) {
	dir := t.TempDir()
	cs := NewCaptureSession(dir, "req-sse-2", "bob", []byte("{}"), "openai")

	chunks := []string{
		`data: {"choices":[{"delta":{"content":"Hi"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":" there"}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}
	for _, chunk := range chunks {
		cs.FeedChunk([]byte(chunk))
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	var rec ConversationRecord
	json.Unmarshal(data, &rec) //nolint:errcheck
	if rec.Response != "Hi there" {
		t.Errorf("streaming response: got %q, want %q", rec.Response, "Hi there")
	}
}

// ---------------------------------------------------------------------------
// Flush 幂等
// ---------------------------------------------------------------------------

func TestCaptureSession_Flush_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cs := NewCaptureSession(dir, "req-idem", "alice", []byte("{}"), "anthropic")

	// 多次 Flush 只写一个文件
	for i := 0; i < 5; i++ {
		cs.Flush()
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("Flush idempotency: expected 1 file, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// 文件名格式：包含 timestamp 和 reqID
// ---------------------------------------------------------------------------

func TestCaptureSession_FileName_ContainsTimestampAndReqID(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()
	cs := NewCaptureSession(dir, "my-request-id", "alice", []byte("{}"), "anthropic")
	cs.Flush()
	after := time.Now()
	_ = before
	_ = after

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	name := entries[0].Name()
	if len(name) == 0 {
		t.Fatal("empty file name")
	}
	// 文件名应包含请求 ID
	if !contains(name, "my-request-id") {
		t.Errorf("file name %q should contain request ID", name)
	}
	// 文件名应以 .json 结尾
	if filepath.Ext(name) != ".json" {
		t.Errorf("file name %q should end with .json", name)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsRune(s, substr))
}

func containsRune(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
