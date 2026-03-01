package lb

import (
	"testing"
)

func TestWeightedPickSingleHealthy(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
	})
	got, err := b.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if got.ID != "a" {
		t.Errorf("got ID=%q, want 'a'", got.ID)
	}
}

func TestAllUnhealthy(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: false},
		{ID: "b", Addr: "http://b", Weight: 2, Healthy: false},
	})
	_, err := b.Pick()
	if err != ErrNoHealthyTarget {
		t.Errorf("err = %v, want ErrNoHealthyTarget", err)
	}
}

func TestSkipUnhealthy(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: false},
		{ID: "b", Addr: "http://b", Weight: 1, Healthy: true},
	})
	for i := 0; i < 20; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.ID != "b" {
			t.Errorf("iteration %d: got %q, want 'b'", i, got.ID)
		}
	}
}

func TestWeightedDistribution(t *testing.T) {
	// 权重 1:3，采样 4000 次，预期 b 出现约 75%
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b", Weight: 3, Healthy: true},
	})

	counts := map[string]int{}
	const N = 4000
	for i := 0; i < N; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		counts[got.ID]++
	}

	ratioB := float64(counts["b"]) / float64(N)
	// 期望 75%，允许 ±10%（即 65%~85%）
	if ratioB < 0.65 || ratioB > 0.85 {
		t.Errorf("b pick ratio = %.2f, want ~0.75 (±0.10); counts=%v", ratioB, counts)
	}
}

func TestMarkHealthyUnhealthy(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b", Weight: 1, Healthy: true},
	})

	// 标记 a 为不健康
	b.MarkUnhealthy("a")
	for i := 0; i < 10; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick after MarkUnhealthy: %v", err)
		}
		if got.ID != "b" {
			t.Errorf("got %q, want 'b' after marking 'a' unhealthy", got.ID)
		}
	}

	// 恢复 a 为健康
	b.MarkHealthy("a")
	sawA := false
	for i := 0; i < 50; i++ {
		got, _ := b.Pick()
		if got.ID == "a" {
			sawA = true
			break
		}
	}
	if !sawA {
		t.Error("expected 'a' to be picked after MarkHealthy, but never selected in 50 tries")
	}
}

func TestUpdateTargets(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
	})

	// 替换目标列表
	b.UpdateTargets([]Target{
		{ID: "x", Addr: "http://x", Weight: 1, Healthy: true},
		{ID: "y", Addr: "http://y", Weight: 1, Healthy: true},
	})

	// 旧节点 a 不应出现
	ids := map[string]bool{}
	for i := 0; i < 40; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		ids[got.ID] = true
	}
	if ids["a"] {
		t.Error("old target 'a' should not appear after UpdateTargets")
	}
	if !ids["x"] && !ids["y"] {
		t.Error("neither 'x' nor 'y' was selected after UpdateTargets")
	}
}

func TestNormalizeWeights(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 0, Healthy: true},  // 应被修正为 1
		{ID: "b", Addr: "http://b", Weight: -5, Healthy: true}, // 应被修正为 1
	})
	// 不应 panic，且能正常 Pick
	got, err := b.Pick()
	if err != nil {
		t.Fatalf("Pick with zero/negative weight: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil target")
	}
}

func TestTargetsSnapshot(t *testing.T) {
	original := []Target{
		{ID: "a", Addr: "http://a", Weight: 2, Healthy: true},
	}
	b := NewWeightedRandom(original)

	snap := b.Targets()
	if len(snap) != 1 || snap[0].ID != "a" {
		t.Errorf("Targets() = %v, want [{a ...}]", snap)
	}

	// 修改快照不应影响内部状态
	snap[0].Healthy = false
	got, err := b.Pick()
	if err != nil {
		t.Errorf("Pick should still succeed after modifying snapshot: %v", err)
	}
	_ = got
}

// ---------------------------------------------------------------------------
// Drain tests
// ---------------------------------------------------------------------------

func TestSetDraining(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b", Weight: 1, Healthy: true},
	})

	// 初始状态：两个节点都可用
	for i := 0; i < 10; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.ID != "a" && got.ID != "b" {
			t.Errorf("got unexpected ID %q", got.ID)
		}
	}

	// 设置 a 为排水模式
	b.SetDraining("a", true)
	if !b.IsDraining("a") {
		t.Error("IsDraining('a') = false, want true")
	}
	if b.IsDraining("b") {
		t.Error("IsDraining('b') = true, want false")
	}

	// 现在 a 应该被跳过
	for i := 0; i < 10; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick after drain: %v", err)
		}
		if got.ID != "b" {
			t.Errorf("got %q, want 'b' (drained node 'a' should be skipped)", got.ID)
		}
	}

	// 恢复 a
	b.SetDraining("a", false)
	if b.IsDraining("a") {
		t.Error("IsDraining('a') = true, want false after undrain")
	}

	// a 应该重新参与负载均衡
	sawA := false
	for i := 0; i < 50; i++ {
		got, _ := b.Pick()
		if got.ID == "a" {
			sawA = true
			break
		}
	}
	if !sawA {
		t.Error("expected 'a' to be picked after undrain, but never selected in 50 tries")
	}
}

func TestAllDraining(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
		{ID: "b", Addr: "http://b", Weight: 1, Healthy: true},
	})

	// 所有节点都进入排水模式
	b.SetDraining("a", true)
	b.SetDraining("b", true)

	_, err := b.Pick()
	if err != ErrNoHealthyTarget {
		t.Errorf("Pick() error = %v, want ErrNoHealthyTarget when all targets draining", err)
	}
}

func TestDrainingAndUnhealthy(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true, Draining: true},
		{ID: "b", Addr: "http://b", Weight: 1, Healthy: false},
		{ID: "c", Addr: "http://c", Weight: 1, Healthy: true},
	})

	// a 排水，b 不健康，只有 c 可用
	for i := 0; i < 10; i++ {
		got, err := b.Pick()
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.ID != "c" {
			t.Errorf("got %q, want 'c'", got.ID)
		}
	}
}

func TestDrainingNonExistentTarget(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
	})

	// 设置不存在的节点为排水模式（应该是 no-op）
	b.SetDraining("nonexistent", true)

	// a 仍然可用
	got, err := b.Pick()
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if got.ID != "a" {
		t.Errorf("got %q, want 'a'", got.ID)
	}

	// 检查不存在的节点
	if b.IsDraining("nonexistent") {
		t.Error("IsDraining('nonexistent') = true, want false for unknown target")
	}
}

func TestTargetsSnapshotIncludesDraining(t *testing.T) {
	b := NewWeightedRandom([]Target{
		{ID: "a", Addr: "http://a", Weight: 1, Healthy: true},
	})

	b.SetDraining("a", true)

	snap := b.Targets()
	if len(snap) != 1 {
		t.Fatalf("Targets() returned %d targets, want 1", len(snap))
	}
	if !snap[0].Draining {
		t.Error("snapshot should show Draining=true for target 'a'")
	}
}
