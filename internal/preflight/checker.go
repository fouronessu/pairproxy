package preflight

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/l17728/pairproxy/internal/config"
)

// CheckSProxy 对 s-proxy 启动环境做 preflight 检查：
//  1. 数据库目录可写（或可创建）
//  2. 监听端口未被占用
//
// 任何检查失败都会返回包含所有问题的聚合错误（不提前退出）。
func CheckSProxy(logger *zap.Logger, cfg *config.SProxyFullConfig) error {
	l := logger.Named("preflight")
	l.Info("running sproxy preflight checks",
		zap.String("db_path", cfg.Database.Path),
		zap.String("listen", cfg.Listen.Addr()),
	)

	var errs []string

	// 检查数据库路径可写（:memory: 为测试用内存数据库，跳过）
	if cfg.Database.Path != "" && cfg.Database.Path != ":memory:" {
		if err := checkWritable(cfg.Database.Path); err != nil {
			errs = append(errs, err.Error())
			l.Warn("preflight: database path check failed",
				zap.String("path", cfg.Database.Path),
				zap.Error(err),
			)
		} else {
			l.Debug("preflight: database path is writable",
				zap.String("path", cfg.Database.Path),
			)
		}
	}

	// 检查监听端口未被占用
	addr := cfg.Listen.Addr()
	if err := checkPortAvailable(addr); err != nil {
		errs = append(errs, err.Error())
		l.Warn("preflight: listen address check failed",
			zap.String("addr", addr),
			zap.Error(err),
		)
	} else {
		l.Debug("preflight: listen address is available", zap.String("addr", addr))
	}

	if len(errs) > 0 {
		l.Error("sproxy preflight checks failed",
			zap.Strings("errors", errs),
		)
		return fmt.Errorf("preflight checks failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	l.Info("sproxy preflight checks passed")
	return nil
}

// CheckCProxy 对 c-proxy 启动环境做 preflight 检查：
//  1. 监听端口未被占用
func CheckCProxy(logger *zap.Logger, cfg *config.CProxyConfig) error {
	l := logger.Named("preflight")
	l.Info("running cproxy preflight checks",
		zap.String("listen", cfg.Listen.Addr()),
	)

	var errs []string

	addr := cfg.Listen.Addr()
	if err := checkPortAvailable(addr); err != nil {
		errs = append(errs, err.Error())
		l.Warn("preflight: listen address check failed",
			zap.String("addr", addr),
			zap.Error(err),
		)
	} else {
		l.Debug("preflight: listen address is available", zap.String("addr", addr))
	}

	if len(errs) > 0 {
		l.Error("cproxy preflight checks failed",
			zap.Strings("errors", errs),
		)
		return fmt.Errorf("preflight checks failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	l.Info("cproxy preflight checks passed")
	return nil
}

// checkWritable 确认 filePath 的父目录存在且可写。
// 通过尝试创建并立即删除临时文件来验证写权限。
func checkWritable(filePath string) error {
	dir := filepath.Dir(filePath)
	if dir == "" || dir == "." {
		dir = "."
	}

	// 创建目录（如果不存在）
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cannot create database directory %q: %w", dir, err)
	}

	// 尝试创建临时文件来验证写权限
	testPath := filePath + ".preflight_tmp"
	f, err := os.OpenFile(testPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("database path %q is not writable: %w", filePath, err)
	}
	f.Close()
	os.Remove(testPath) //nolint:errcheck
	return nil
}

// checkPortAvailable 通过尝试监听来确认 addr 未被占用。
func checkPortAvailable(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen address %q is already in use: %w", addr, err)
	}
	ln.Close()
	return nil
}
