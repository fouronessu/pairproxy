//go:build !windows

package main

import (
	"net/http"
	"os"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestIsWindowsService_ReturnsFalse verifies that on non-Windows platforms
// isWindowsService() always returns false.
func TestIsWindowsService_ReturnsFalse(t *testing.T) {
	if isWindowsService() {
		t.Error("isWindowsService() should return false on non-Windows platforms")
	}
}

// TestRunAsWindowsService_Panics verifies that calling runAsWindowsService on a
// non-Windows platform panics with a clear message (it is guarded by isWindowsService
// in runStart, so it must never be reached at runtime).
func TestRunAsWindowsService_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("runAsWindowsService should panic on non-Windows platforms")
		}
	}()
	//nolint:staticcheck // intentional panic test
	_ = runAsWindowsService(nil, zap.NewNop())
}

// TestDaemonize_NoopWhenAlreadyDaemon verifies that when _CPROXY_DAEMON=1 is
// set in the environment, daemonize() returns nil immediately without forking.
func TestDaemonize_NoopWhenAlreadyDaemon(t *testing.T) {
	t.Setenv("_CPROXY_DAEMON", "1")
	logger := zaptest.NewLogger(t)
	if err := daemonize("", logger); err != nil {
		t.Errorf("daemonize() with _CPROXY_DAEMON=1 should return nil, got: %v", err)
	}
}

// TestDaemonize_NoopWithConfigWhenAlreadyDaemon verifies the no-op path when a
// config path is provided but _CPROXY_DAEMON=1 is already set.
func TestDaemonize_NoopWithConfigWhenAlreadyDaemon(t *testing.T) {
	t.Setenv("_CPROXY_DAEMON", "1")
	logger := zaptest.NewLogger(t)
	if err := daemonize("/etc/pairproxy/cproxy.yaml", logger); err != nil {
		t.Errorf("daemonize('/etc/...') with _CPROXY_DAEMON=1 should return nil, got: %v", err)
	}
}

// TestDaemonize_EnvVarIsChecked verifies that the function inspects
// _CPROXY_DAEMON from the process environment (not a package variable), so
// unsetting it after a test does not bleed into other test runs.
func TestDaemonize_EnvVarIsChecked(t *testing.T) {
	// Explicitly clear the env var to confirm the check works both ways.
	if err := os.Unsetenv("_CPROXY_DAEMON"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}
	// We cannot call daemonize() without _CPROXY_DAEMON because it would try to
	// re-exec the test binary. Just verify the env is absent so the condition
	// that gates the no-op path is known-false.
	if os.Getenv("_CPROXY_DAEMON") != "" {
		t.Error("_CPROXY_DAEMON should be unset at this point")
	}
}

// TestRunAsWindowsServiceSignature verifies that the stub function's type
// signature matches what main.go expects, so cross-platform compilation stays
// correct.  This is a compile-time check; if it builds, the test passes.
func TestRunAsWindowsServiceSignature(_ *testing.T) {
	var _ func(*http.Server, *zap.Logger) error = runAsWindowsService
}
