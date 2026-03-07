package cluster

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/lb"
)

func TestPeerRegisterAndList(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	registry.Register("sp-3", "http://sp-3:9000", "sp-3", 2)

	peers := registry.Peers()
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}

	peerIDs := map[string]bool{}
	for _, p := range peers {
		peerIDs[p.ID] = true
		if !p.IsHealthy {
			t.Errorf("peer %s should be healthy after registration", p.ID)
		}
	}
	if !peerIDs["sp-2"] || !peerIDs["sp-3"] {
		t.Errorf("expected sp-2 and sp-3 in peers, got %v", peerIDs)
	}
}

func TestPeerDeregister(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	registry.Register("sp-3", "http://sp-3:9000", "sp-3", 1)
	registry.Deregister("sp-2")

	peers := registry.Peers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer after deregister, got %d", len(peers))
	}
	if peers[0].ID != "sp-3" {
		t.Errorf("expected sp-3 after deregister, got %s", peers[0].ID)
	}
}

func TestPeerEvictStale(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)
	registry.ttl = 50 * time.Millisecond // 很短的 TTL

	registry.Register("old", "http://old:9000", "old", 1)

	// 等待超时
	time.Sleep(100 * time.Millisecond)

	registry.EvictStale()

	peers := registry.Peers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers after eviction, got %d", len(peers))
	}
}

func TestPeerHeartbeatUpdatesLastSeen(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)
	registry.ttl = 50 * time.Millisecond

	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	time.Sleep(30 * time.Millisecond)

	// 再次 Register = 心跳，更新 LastSeen
	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	time.Sleep(30 * time.Millisecond)

	// TTL=50ms，但总时间=60ms，最后心跳是30ms前，未超时
	registry.EvictStale()

	peers := registry.Peers()
	if len(peers) != 1 {
		t.Errorf("peer should still be alive after heartbeat, got %d peers", len(peers))
	}
}

func TestPeerSyncToBalancer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	registry.Register("sp-3", "http://sp-3:9000", "sp-3", 1)

	// Balancer 应该有 2 个目标
	targets := balancer.Targets()
	if len(targets) != 2 {
		t.Fatalf("balancer should have 2 targets, got %d", len(targets))
	}

	registry.Deregister("sp-3")
	targets = balancer.Targets()
	if len(targets) != 1 {
		t.Fatalf("balancer should have 1 target after deregister, got %d", len(targets))
	}
	if targets[0].ID != "sp-2" {
		t.Errorf("remaining target should be sp-2, got %s", targets[0].ID)
	}
}

// ---------------------------------------------------------------------------
// TestSetSelfTarget — SetSelfTarget (0% coverage)
// ---------------------------------------------------------------------------

// TestSetSelfTarget_PlacedFirst 验证设置 selfTarget 后，syncToManager 将其置于路由表首位。
func TestSetSelfTarget_PlacedFirst(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	selfTarget := lb.Target{ID: "sp-1", Addr: "http://sp-1:9000", Weight: 1, Healthy: true}
	registry.SetSelfTarget(selfTarget)

	// 注册一个 peer
	registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)

	// 路由表中 sp-1 应在首位
	rt := mgr.CurrentTable()
	if len(rt.Entries) < 2 {
		t.Fatalf("expected ≥2 entries after SetSelfTarget + Register, got %d", len(rt.Entries))
	}
	if rt.Entries[0].ID != "sp-1" {
		t.Errorf("selfTarget should be first in routing table, got %q", rt.Entries[0].ID)
	}
}

// TestSetSelfTarget_NoPeers_SelfAlwaysPresent 验证即使没有 peer，self 仍出现在路由表中。
func TestSetSelfTarget_NoPeers_SelfAlwaysPresent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	selfTarget := lb.Target{ID: "sp-1-only", Addr: "http://sp-1-only:9000", Weight: 2, Healthy: true}
	registry.SetSelfTarget(selfTarget)

	// 触发 syncToManager
	registry.Register("tmp-peer", "http://tmp:9000", "tmp", 1)
	registry.Deregister("tmp-peer")

	// 路由表中应只有 sp-1-only
	rt := mgr.CurrentTable()
	if len(rt.Entries) != 1 {
		t.Fatalf("expected 1 entry (selfTarget only), got %d", len(rt.Entries))
	}
	if rt.Entries[0].ID != "sp-1-only" {
		t.Errorf("expected selfTarget in routing table, got %q", rt.Entries[0].ID)
	}
}

// TestSetSelfTarget_WorkerHeartbeatCannotRemoveSelf 验证 worker 心跳不会从路由表中抹去 primary。
func TestSetSelfTarget_WorkerHeartbeatCannotRemoveSelf(t *testing.T) {
	logger := zaptest.NewLogger(t)
	balancer := lb.NewWeightedRandom(nil)
	mgr := NewManager(logger, balancer, nil, "")
	registry := NewPeerRegistry(logger, mgr)

	selfTarget := lb.Target{ID: "sp-1", Addr: "http://sp-1:9000", Weight: 1, Healthy: true}
	registry.SetSelfTarget(selfTarget)

	// 多次 worker 心跳
	for i := 0; i < 5; i++ {
		registry.Register("sp-2", "http://sp-2:9000", "sp-2", 1)
	}

	rt := mgr.CurrentTable()
	found := false
	for _, e := range rt.Entries {
		if e.ID == "sp-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("selfTarget sp-1 should always be present in routing table after worker heartbeat")
	}
}
