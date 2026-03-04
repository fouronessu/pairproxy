package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiter_AllowUnderLimit(t *testing.T) {
	l := NewLoginLimiter(5, time.Minute, 5*time.Minute)

	// maxFail-1 次失败不触发锁定
	for i := 0; i < 4; i++ {
		l.RecordFailure("1.2.3.4")
	}

	allowed, retryAfter := l.Check("1.2.3.4")
	if !allowed {
		t.Errorf("should be allowed after %d failures, got retryAfter=%v", 4, retryAfter)
	}
}

func TestLoginLimiter_LockAfterMaxFails(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 5*time.Minute)

	for i := 0; i < 3; i++ {
		l.RecordFailure("10.0.0.1")
	}

	allowed, retryAfter := l.Check("10.0.0.1")
	if allowed {
		t.Error("should be locked after 3 failures")
	}
	if retryAfter <= 0 || retryAfter > 5*time.Minute {
		t.Errorf("retryAfter out of range: %v", retryAfter)
	}
}

func TestLoginLimiter_ResetOnSuccess(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 5*time.Minute)

	// 达到阈值前的失败
	l.RecordFailure("192.168.1.1")
	l.RecordFailure("192.168.1.1")

	l.RecordSuccess("192.168.1.1")

	// 再次失败不应立即锁定（计数已清零）
	l.RecordFailure("192.168.1.1")
	l.RecordFailure("192.168.1.1")

	allowed, _ := l.Check("192.168.1.1")
	if !allowed {
		t.Error("after success+reset, should allow 2 failures without lockout")
	}
}

func TestLoginLimiter_UnlockAfterLockDuration(t *testing.T) {
	// 使用极短的锁定时长测试解锁
	l := NewLoginLimiter(2, time.Minute, 50*time.Millisecond)

	l.RecordFailure("5.5.5.5")
	l.RecordFailure("5.5.5.5")

	// 刚锁定时应拒绝
	allowed, _ := l.Check("5.5.5.5")
	if allowed {
		t.Error("should be locked immediately after max failures")
	}

	// 等待锁定超时
	time.Sleep(60 * time.Millisecond)

	allowed, _ = l.Check("5.5.5.5")
	if !allowed {
		t.Error("should be unlocked after lockFor duration")
	}
}

func TestLoginLimiter_WindowExpiry(t *testing.T) {
	// 使用极短窗口
	l := NewLoginLimiter(3, 50*time.Millisecond, 5*time.Minute)

	l.RecordFailure("8.8.8.8")
	l.RecordFailure("8.8.8.8")

	// 等待窗口过期
	time.Sleep(60 * time.Millisecond)

	// 窗口过期后，再次失败应重新计数，不触发锁定
	l.RecordFailure("8.8.8.8")

	allowed, _ := l.Check("8.8.8.8")
	if !allowed {
		t.Error("after window expiry, failure count should reset")
	}
}

func TestLoginLimiter_DifferentIPs(t *testing.T) {
	l := NewLoginLimiter(2, time.Minute, 5*time.Minute)

	l.RecordFailure("1.1.1.1")
	l.RecordFailure("1.1.1.1")

	// 1.1.1.1 已锁定，但 2.2.2.2 不受影响
	allowed1, _ := l.Check("1.1.1.1")
	if allowed1 {
		t.Error("1.1.1.1 should be locked")
	}

	allowed2, _ := l.Check("2.2.2.2")
	if !allowed2 {
		t.Error("2.2.2.2 should not be affected by 1.1.1.1's lockout")
	}
}

func TestLoginLimiter_Purge(t *testing.T) {
	l := NewLoginLimiter(2, 50*time.Millisecond, 50*time.Millisecond)

	l.RecordFailure("3.3.3.3")
	l.RecordFailure("3.3.3.3") // 触发锁定

	// 等待锁定超时
	time.Sleep(60 * time.Millisecond)

	// Purge 应清除过期条目
	l.Purge()

	l.mu.Lock()
	_, ok := l.entries["3.3.3.3"]
	l.mu.Unlock()
	if ok {
		t.Error("Purge should have removed expired entry")
	}
}

func TestRealIP_TrustedProxy_UsesXFF(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	proxies := []net.IPNet{*cidr}

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
	req.RemoteAddr = "10.0.0.2:12345" // trusted proxy

	ip := realIP(req, proxies)
	if ip != "203.0.113.1" {
		t.Errorf("realIP = %q, want %q (should use XFF from trusted proxy)", ip, "203.0.113.1")
	}
}

func TestRealIP_UntrustedProxy_IgnoresXFF(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	proxies := []net.IPNet{*cidr}

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // attacker's forged XFF
	req.RemoteAddr = "203.0.113.99:12345"          // NOT a trusted proxy

	ip := realIP(req, proxies)
	if ip != "203.0.113.99" {
		t.Errorf("realIP = %q, want %q (should ignore XFF from untrusted proxy)", ip, "203.0.113.99")
	}
}

func TestRealIP_EmptyTrustedProxies_AlwaysRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.RemoteAddr = "5.6.7.8:12345"

	ip := realIP(req, nil) // empty: never trust XFF
	if ip != "5.6.7.8" {
		t.Errorf("realIP = %q, want %q (empty proxies should always use RemoteAddr)", ip, "5.6.7.8")
	}
}

func TestRealIP_RemoteAddr_NoHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "172.16.0.5:54321"

	ip := realIP(req, nil)
	if ip != "172.16.0.5" {
		t.Errorf("realIP = %q, want %q", ip, "172.16.0.5")
	}
}

// TestRateLimiter_SpoofedXFF_BlocksRealIP verifies that after the fix,
// an attacker cycling through spoofed XFF headers cannot bypass rate limiting
// because realIP() always returns RemoteAddr when no trusted proxy is configured.
func TestRateLimiter_SpoofedXFF_BlocksRealIP(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 5*time.Minute)
	var proxies []net.IPNet // no trusted proxies → XFF always ignored

	attackerRealIP := "1.2.3.4"

	spoofHeaders := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, xff := range spoofHeaders {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.Header.Set("X-Forwarded-For", xff)
		req.RemoteAddr = attackerRealIP + ":12345"

		ip := realIP(req, proxies)
		l.RecordFailure(ip)
	}

	// After 3 failures all from the real IP, the real IP must be locked
	allowed, _ := l.Check(attackerRealIP)
	if allowed {
		t.Error("real IP should be locked after 3 failures regardless of spoofed XFF headers")
	}
}
