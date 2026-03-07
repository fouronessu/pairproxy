package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"

	"github.com/l17728/pairproxy/internal/api"
	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/dashboard"
	"github.com/l17728/pairproxy/internal/db"
)

// TestHandleToggleActive tests user activation/deactivation
func TestHandleToggleActive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test user
	userRepo.Create(&db.User{ID: "user1", Username: "testuser", IsActive: true})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("toggle_active", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/active", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}

		// Verify user is now inactive
		user, _ := userRepo.GetByID("user1")
		if user.IsActive {
			t.Error("user should be inactive after toggle")
		}
	})
}

// TestHandleResetPassword tests password reset functionality
func TestHandleResetPassword(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test user
	userRepo.Create(&db.User{ID: "user1", Username: "testuser"})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("reset_password", func(t *testing.T) {
		form := url.Values{}
		form.Set("new_password", "newpass123")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})

	t.Run("missing_password", func(t *testing.T) {
		form := url.Values{}

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}

		loc := rr.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error in redirect, got %q", loc)
		}
	})
}

// TestHandleSetUserGroup tests setting user group
func TestHandleSetUserGroup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test user and group
	userRepo.Create(&db.User{ID: "user1", Username: "testuser"})
	groupRepo.Create(&db.Group{Name: "testgroup"})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("set_group", func(t *testing.T) {
		form := url.Values{}
		form.Set("group_name", "testgroup")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/group", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})

	t.Run("clear_group", func(t *testing.T) {
		form := url.Values{}
		form.Set("group_name", "")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/group", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleSetQuota tests setting group quota
func TestHandleSetQuota(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test group
	groupRepo.Create(&db.Group{Name: "testgroup"})
	groups, _ := groupRepo.List()
	if len(groups) == 0 {
		t.Fatal("no groups found")
	}
	group := groups[0]

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("set_quota", func(t *testing.T) {
		form := url.Values{}
		form.Set("daily_token_limit", "1000000")
		form.Set("monthly_token_limit", "30000000")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/groups/"+group.ID+"/quota", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleDeleteGroup tests group deletion
func TestHandleDeleteGroup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test group
	groupRepo.Create(&db.Group{Name: "testgroup"})
	groups, _ := groupRepo.List()
	if len(groups) == 0 {
		t.Fatal("no groups found")
	}
	group := groups[0]

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("delete_group", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/groups/"+group.ID+"/delete", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleAuditPage tests audit log page
func TestHandleAuditPage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("audit_page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/audit", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}

// TestHandleRevokeUserTokens tests token revocation
func TestHandleRevokeUserTokens(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	tokenRepo := db.NewRefreshTokenRepo(gormDB, logger)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	// Create test user
	userRepo.Create(&db.User{ID: "user1", Username: "testuser"})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)
	h.SetTokenRepo(tokenRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("revoke_tokens_success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/revoke-tokens", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}

		loc := rr.Header().Get("Location")
		if !strings.Contains(loc, "flash=") {
			t.Errorf("expected flash message in redirect, got %q", loc)
		}
	})

	t.Run("token_repo_not_configured", func(t *testing.T) {
		h2 := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

		mux2 := http.NewServeMux()
		h2.RegisterRoutes(mux2)

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/user1/revoke-tokens", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}

		loc := rr.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error in redirect, got %q", loc)
		}
	})
}

// ---------------------------------------------------------------------------
// TestHandleResetPassword_CorrectFieldName — uses "password" field (not "new_password")
// ---------------------------------------------------------------------------

func TestHandleResetPassword_PasswordFieldEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	jwtMgr, err := auth.NewManager(logger, "test-reset-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	userRepo.Create(&db.User{ID: "u-reset-1", Username: "reset-user-1", PasswordHash: "x", IsActive: true})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{UserID: "__admin__", Username: "admin", Role: "admin"}, time.Hour)

	// 空 password 字段 → redirect with error
	form := url.Values{}
	form.Set("password", "")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/users/u-reset-1/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("Location = %q, expected error param for empty password", loc)
	}
}

func TestHandleResetPassword_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	jwtMgr, err := auth.NewManager(logger, "test-reset-secret2")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	userRepo.Create(&db.User{ID: "u-reset-2", Username: "reset-user-2", PasswordHash: "x", IsActive: true})

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{UserID: "__admin__", Username: "admin", Role: "admin"}, time.Hour)

	// 有效的新密码 → 成功重定向，含 flash 消息
	form := url.Values{}
	form.Set("password", "new-secure-pass-999")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/users/u-reset-2/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if strings.Contains(loc, "error=") {
		t.Errorf("Location = %q, should NOT contain error on success", loc)
	}
}

func TestHandleResetPassword_NonExistentUser_UpdateFails(t *testing.T) {
	logger := zaptest.NewLogger(t)
	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	jwtMgr, err := auth.NewManager(logger, "test-reset-secret3")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	// 注意：不创建任何 user

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{UserID: "__admin__", Username: "admin", Role: "admin"}, time.Hour)

	// 有效密码，但用户不存在 → UpdatePassword 会失败还是成功？
	// GORM Update 对不存在的 ID 不报错（rows affected = 0），所以会重定向到 flash
	form := url.Values{}
	form.Set("password", "valid-pass-123")
	req := httptest.NewRequest(http.MethodPost, "/dashboard/users/nonexistent-user-id/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// 无论成功还是失败，都应返回 302
	if rr.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rr.Code)
	}
}
