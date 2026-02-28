//go:build !windows

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"go.uber.org/zap"

	"github.com/l17728/pairproxy/internal/auth"
)

// isWindowsService always returns false on non-Windows platforms.
func isWindowsService() bool { return false }

// runAsWindowsService is never called on non-Windows platforms.
func runAsWindowsService(_ *http.Server, _ *zap.Logger) error {
	panic("runAsWindowsService called on non-Windows platform")
}

// daemonize re-executes the current binary in a new session detached from the
// controlling terminal. The parent process exits after the child is spawned.
//
// If the environment variable _CPROXY_DAEMON=1 is already set the function
// is a no-op — we are already the background child and execution continues
// normally in runStart().
func daemonize(configPath string, logger *zap.Logger) error {
	if os.Getenv("_CPROXY_DAEMON") == "1" {
		// We are the daemon child; continue with normal server startup.
		logger.Debug("running as daemon child, skipping daemonize")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		logger.Debug("resolved executable symlink", zap.String("from", exe), zap.String("to", resolved))
		exe = resolved
	}

	// Child process args: `cproxy start [--config <path>]` (no --daemon flag).
	childArgs := []string{"start"}
	if configPath != "" {
		childArgs = append(childArgs, "--config", configPath)
	}
	logger.Debug("daemonize: building child command", zap.String("exe", exe), zap.Strings("args", childArgs))

	// Write stdout/stderr to the token-directory log file.
	// Fall back to the system temp directory if the token dir is not writable.
	logDir := auth.DefaultTokenDir()
	logPath := filepath.Join(logDir, "cproxy.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger.Warn("could not open log file in token dir, falling back to temp dir",
			zap.String("tried", logPath), zap.Error(err))
		logPath = filepath.Join(os.TempDir(), "cproxy.log")
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file %q: %w", logPath, err)
		}
	}
	logger.Debug("daemon log file opened", zap.String("path", logPath))

	cmd := exec.Command(exe, childArgs...)
	cmd.Env = append(os.Environ(), "_CPROXY_DAEMON=1")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Setsid creates a new session so the child is fully detached from the
	// terminal and will not receive SIGHUP when the parent's session ends.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start background process: %w", err)
	}
	logFile.Close()

	pid := cmd.Process.Pid
	logger.Info("daemon child spawned", zap.Int("pid", pid), zap.String("log", logPath))

	pidPath := filepath.Join(logDir, "cproxy.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		// Non-fatal: PID file is a convenience only.
		logger.Warn("could not write PID file", zap.String("path", pidPath), zap.Error(err))
		pidPath = ""
	}

	fmt.Printf("✓ cproxy started in background (PID %d)\n", pid)
	fmt.Printf("  Logs: %s\n", logPath)
	if pidPath != "" {
		fmt.Printf("  PID:  %s\n", pidPath)
		fmt.Printf("  Stop: kill $(cat %s)\n", pidPath)
	} else {
		fmt.Printf("  Stop: kill %d\n", pid)
	}

	// The parent's work is done; exit cleanly.
	os.Exit(0)
	return nil // unreachable
}
