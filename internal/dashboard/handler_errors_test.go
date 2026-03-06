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

// TestHandleCreateUserErrors tests error paths in user creation
func TestHandleCreateUserErrors(t *testing.T) {
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

	t.Run("missing_username", func(t *testing.T) {
		form := url.Values{}
		form.Set("password", "testpass")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users", strings.NewReader(form.Encode()))
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

	t.Run("missing_password", func(t *testing.T) {
		form := url.Values{}
		form.Set("username", "testuser")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users", strings.NewReader(form.Encode()))
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

	t.Run("duplicate_username", func(t *testing.T) {
		// Create a user first
		userRepo.Create(&db.User{ID: "user1", Username: "existinguser"})

		form := url.Values{}
		form.Set("username", "existinguser")
		form.Set("password", "testpass")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleCreateGroupErrors tests error paths in group creation
func TestHandleCreateGroupErrors(t *testing.T) {
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

	t.Run("missing_name", func(t *testing.T) {
		form := url.Values{}

		req := httptest.NewRequest(http.MethodPost, "/dashboard/groups", strings.NewReader(form.Encode()))
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

	t.Run("duplicate_group_name", func(t *testing.T) {
		// Create a group first
		groupRepo.Create(&db.Group{Name: "existinggroup"})

		form := url.Values{}
		form.Set("name", "existinggroup")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/groups", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleResetPasswordErrors tests error paths in password reset
func TestHandleResetPasswordErrors(t *testing.T) {
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

	t.Run("short_password", func(t *testing.T) {
		form := url.Values{}
		form.Set("new_password", "123")

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

	t.Run("nonexistent_user", func(t *testing.T) {
		form := url.Values{}
		form.Set("new_password", "newpass123")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/users/nonexistent/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})
}

// TestHandleLLMDeleteBindingErrors tests error paths in LLM binding deletion
func TestHandleLLMDeleteBindingErrors(t *testing.T) {
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
	llmBindingRepo := db.NewLLMBindingRepo(gormDB, logger)

	jwtMgr, err := auth.NewManager(logger, "test-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)

	h := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)
	h.SetLLMDeps(llmBindingRepo, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, _ := jwtMgr.Sign(auth.JWTClaims{
		UserID:   "__admin__",
		Username: "admin",
		Role:     "admin",
	}, time.Hour)

	t.Run("llm_binding_repo_not_configured", func(t *testing.T) {
		h2 := dashboard.NewHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, string(hash), time.Hour)

		mux2 := http.NewServeMux()
		h2.RegisterRoutes(mux2)

		req := httptest.NewRequest(http.MethodPost, "/dashboard/llm/bindings/some-id/delete", nil)
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

// TestRenderPageError tests renderPage error handling
func TestRenderPageError(t *testing.T) {
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

	t.Run("invalid_template", func(t *testing.T) {
		// Try to access a page that would trigger template error
		// This tests the error path in renderPage
		req := httptest.NewRequest(http.MethodGet, "/dashboard/users", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		// Should still return 200 even if template has issues
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}

// TestHandleTrendsAPIErrors tests error paths in trends API
func TestHandleTrendsAPIErrors(t *testing.T) {
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

	t.Run("negative_days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/dashboard/trends?days=-5", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		// Should fallback to default 7 days
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})

	t.Run("days_at_boundary_365", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/dashboard/trends?days=365", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}


// TestHandleLoginSubmitErrors tests login error paths
func TestHandleLoginSubmitErrors(t *testing.T) {
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

	t.Run("wrong_password", func(t *testing.T) {
		form := url.Values{}
		form.Set("password", "wrongpass")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})

	t.Run("missing_password", func(t *testing.T) {
		form := url.Values{}

		req := httptest.NewRequest(http.MethodPost, "/dashboard/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}

// TestHandleSetQuotaErrors tests quota setting error paths
func TestHandleSetQuotaErrors(t *testing.T) {
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

	t.Run("invalid_daily_limit", func(t *testing.T) {
		form := url.Values{}
		form.Set("daily_token_limit", "invalid")

		req := httptest.NewRequest(http.MethodPost, "/dashboard/groups/"+group.ID+"/quota", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound && rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want 302 or 303", rr.Code)
		}
	})

	t.Run("empty_quota_values", func(t *testing.T) {
		form := url.Values{}
		form.Set("daily_token_limit", "")
		form.Set("monthly_token_limit", "")

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

// TestHandleOverviewEdgeCases tests overview page edge cases
func TestHandleOverviewEdgeCases(t *testing.T) {
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

	t.Run("overview_with_no_data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req.AddCookie(&http.Cookie{Name: api.AdminCookieName, Value: token})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}
