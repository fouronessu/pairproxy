package preflight

import (
	"net"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/config"
)

// ---------------------------------------------------------------------------
// checkPortAvailable 测试
// ---------------------------------------------------------------------------

func TestCheckPortAvailable_FreePort(t *testing.T) {
	// 绑定到 :0（随机空闲端口），获取地址后立即释放，再检查是否可用
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	addr := ln.Addr().String()
	ln.Close() // 释放端口

	if err := checkPortAvailable(addr); err != nil {
		t.Errorf("checkPortAvailable(%q): unexpected error: %v", addr, err)
	}
}

func TestCheckPortAvailable_BusyPort(t *testing.T) {
	// 绑定一个端口并保持占用
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if err := checkPortAvailable(addr); err == nil {
		t.Errorf("checkPortAvailable(%q): expected error for in-use port, got nil", addr)
	}
}

// ---------------------------------------------------------------------------
// checkWritable 测试
// ---------------------------------------------------------------------------

func TestCheckWritable_ValidPath(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/test.db"
	if err := checkWritable(filePath); err != nil {
		t.Errorf("checkWritable(%q): unexpected error: %v", filePath, err)
	}
}

func TestCheckWritable_MemoryPath(t *testing.T) {
	// :memory: 路径应由调用方跳过，但直接调用 checkWritable 会尝试创建文件
	// 本测试仅验证其他路径正常工作；:memory: 的跳过逻辑在 CheckSProxy 层测试
}

// ---------------------------------------------------------------------------
// CheckSProxy 测试
// ---------------------------------------------------------------------------

func TestCheckSProxy_Valid(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// 获取一个空闲端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := &config.SProxyFullConfig{}
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port
	cfg.Database.Path = t.TempDir() + "/test.db"

	if err := CheckSProxy(logger, cfg); err != nil {
		t.Errorf("CheckSProxy: unexpected error: %v", err)
	}
}

func TestCheckSProxy_PortInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &config.SProxyFullConfig{}
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port
	cfg.Database.Path = t.TempDir() + "/test.db"

	err = CheckSProxy(logger, cfg)
	if err == nil {
		t.Fatal("CheckSProxy: expected error for in-use port, got nil")
	}
}

func TestCheckSProxy_MemoryDB_SkipsWriteCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := &config.SProxyFullConfig{}
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port
	cfg.Database.Path = ":memory:"

	// :memory: 不做写检查，不应返回错误
	if err := CheckSProxy(logger, cfg); err != nil {
		t.Errorf("CheckSProxy with :memory: db: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CheckCProxy 测试
// ---------------------------------------------------------------------------

func TestCheckCProxy_Valid(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := &config.CProxyConfig{}
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port

	if err := CheckCProxy(logger, cfg); err != nil {
		t.Errorf("CheckCProxy: unexpected error: %v", err)
	}
}

func TestCheckCProxy_PortInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind test socket")
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &config.CProxyConfig{}
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port

	if err := CheckCProxy(logger, cfg); err == nil {
		t.Fatal("CheckCProxy: expected error for in-use port, got nil")
	}
}
