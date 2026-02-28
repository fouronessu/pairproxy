//go:build windows

package main

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sys/windows/svc"
)

// TestIsWindowsService_ReturnsFalse verifies that isWindowsService() returns
// false when cproxy is started normally (not by the Windows SCM).
func TestIsWindowsService_ReturnsFalse(t *testing.T) {
	if isWindowsService() {
		t.Error("isWindowsService() should return false when not launched by the SCM")
	}
}

// TestDaemonize_WindowsReturnsError verifies that daemonize() returns an
// actionable error on Windows, directing the user to install-service.
func TestDaemonize_WindowsReturnsError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	err := daemonize("", logger)
	if err == nil {
		t.Fatal("daemonize() should return an error on Windows")
	}
	if !strings.Contains(err.Error(), "install-service") {
		t.Errorf("error should mention 'install-service', got: %q", err.Error())
	}
}

// TestDaemonize_WindowsReturnsErrorWithConfig verifies the same regardless of
// whether a config path was provided.
func TestDaemonize_WindowsReturnsErrorWithConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	err := daemonize(`C:\ProgramData\pairproxy\cproxy.yaml`, logger)
	if err == nil {
		t.Fatal("daemonize() with config should return an error on Windows")
	}
	if !strings.Contains(err.Error(), "install-service") {
		t.Errorf("error should mention 'install-service', got: %q", err.Error())
	}
}

// TestRunAsWindowsServiceSignature is a compile-time check that the function
// signature matches what main.go expects.
func TestRunAsWindowsServiceSignature(_ *testing.T) {
	var _ func(*http.Server, *zap.Logger) error = runAsWindowsService
}

// freeTCPAddr returns a free localhost address by opening a listener with :0,
// recording the address, and closing it immediately.
// There is a small TOCTOU window, but it is acceptable for tests.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen :0: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// waitForState drains the status channel until the desired SCM state is seen or
// the deadline is exceeded.
func waitForState(t *testing.T, status <-chan svc.Status, want svc.State, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case st := <-status:
			if st.State == want {
				return true
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	return false
}

// TestCProxyServiceExecute_StopSignal verifies that sending svc.Stop causes
// Execute to shut down the HTTP server and return (false, 0).
func TestCProxyServiceExecute_StopSignal(t *testing.T) {
	logger := zaptest.NewLogger(t)
	addr := freeTCPAddr(t)

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	service := &cproxyService{server: server, logger: logger}

	r := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 16)

	type result struct {
		svcEC    bool
		exitCode uint32
	}
	done := make(chan result, 1)
	go func() {
		svcEC, exitCode := service.Execute(nil, r, status)
		done <- result{svcEC, exitCode}
	}()

	if !waitForState(t, status, svc.Running, 3*time.Second) {
		t.Fatal("service did not reach Running state within 3s")
	}

	r <- svc.ChangeRequest{Cmd: svc.Stop}

	select {
	case res := <-done:
		if res.svcEC {
			t.Error("Execute should return svcSpecificEC=false on Stop")
		}
		if res.exitCode != 0 {
			t.Errorf("Execute should return exitCode=0 on Stop, got %d", res.exitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return within 5s after Stop signal")
	}
}

// TestCProxyServiceExecute_ShutdownSignal verifies that svc.Shutdown is
// handled identically to svc.Stop.
func TestCProxyServiceExecute_ShutdownSignal(t *testing.T) {
	logger := zaptest.NewLogger(t)
	addr := freeTCPAddr(t)

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	service := &cproxyService{server: server, logger: logger}

	r := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 16)

	type result struct {
		svcEC    bool
		exitCode uint32
	}
	done := make(chan result, 1)
	go func() {
		svcEC, exitCode := service.Execute(nil, r, status)
		done <- result{svcEC, exitCode}
	}()

	if !waitForState(t, status, svc.Running, 3*time.Second) {
		t.Fatal("service did not reach Running state within 3s")
	}

	r <- svc.ChangeRequest{Cmd: svc.Shutdown}

	select {
	case res := <-done:
		if res.svcEC {
			t.Error("Execute should return svcSpecificEC=false on Shutdown")
		}
		if res.exitCode != 0 {
			t.Errorf("Execute should return exitCode=0 on Shutdown, got %d", res.exitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return within 5s after Shutdown signal")
	}
}

// TestCProxyServiceExecute_ServerFatalError verifies that when the HTTP server
// fails to bind (because the address is already occupied), Execute returns
// (true, 1) indicating a service-specific error code.
func TestCProxyServiceExecute_ServerFatalError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Keep the listener open so ListenAndServe fails with "address already in use".
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen :0: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	service := &cproxyService{server: server, logger: logger}

	r := make(chan svc.ChangeRequest)
	status := make(chan svc.Status, 16)

	type result struct {
		svcEC    bool
		exitCode uint32
	}
	done := make(chan result, 1)
	go func() {
		svcEC, exitCode := service.Execute(nil, r, status)
		done <- result{svcEC, exitCode}
	}()

	select {
	case res := <-done:
		if !res.svcEC {
			t.Error("Execute should return svcSpecificEC=true on server fatal error")
		}
		if res.exitCode != 1 {
			t.Errorf("Execute should return exitCode=1 on fatal error, got %d", res.exitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return within 5s after server fatal error")
	}
}

// TestCProxyServiceExecute_UnknownCmdStaysRunning verifies that an unrecognised
// SCM command does not stop the service; Execute acknowledges it and remains Running.
func TestCProxyServiceExecute_UnknownCmdStaysRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	addr := freeTCPAddr(t)

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	service := &cproxyService{server: server, logger: logger}

	r := make(chan svc.ChangeRequest, 2)
	status := make(chan svc.Status, 16)

	type result struct {
		svcEC    bool
		exitCode uint32
	}
	done := make(chan result, 1)
	go func() {
		svcEC, exitCode := service.Execute(nil, r, status)
		done <- result{svcEC, exitCode}
	}()

	if !waitForState(t, status, svc.Running, 3*time.Second) {
		t.Fatal("service did not reach Running state within 3s")
	}

	// Send an unknown command (e.g., Pause which we don't support).
	r <- svc.ChangeRequest{Cmd: svc.Pause}

	// After the unknown command the service should re-signal Running.
	if !waitForState(t, status, svc.Running, 2*time.Second) {
		t.Error("service should re-signal Running after an unknown SCM command")
	}

	// Service should still be alive; clean up with Stop.
	r <- svc.ChangeRequest{Cmd: svc.Stop}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not stop cleanly after cleanup Stop")
	}
}
