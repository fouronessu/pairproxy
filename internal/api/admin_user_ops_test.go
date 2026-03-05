package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/db"
)

// setupAdminTestWithTokenRepo returns a test env that also has a token repo wired up
// (needed for revoke-tokens tests).
func setupAdminTestWithTokenRepo(t *testing.T) (*AdminHandler, *auth.Manager, *http.ServeMux, *db.UserRepo, *db.GroupRepo) {
	t.Helper()
	logger := zaptest.NewLogger(t)

	jwtMgr, err := auth.NewManager(logger, "ops-secret")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	gormDB, err := db.Open(logger, ":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(logger, gormDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	writer := db.NewUsageWriter(gormDB, logger, 100, time.Minute)
	writer.Start(ctx)
	t.Cleanup(func() { cancel(); writer.Wait() })

	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	tokenRepo := db.NewRefreshTokenRepo(gormDB, logger)

	hash, _ := auth.HashPassword(logger, "adminpass")
	handler := NewAdminHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, hash, time.Hour)
	handler.SetTokenRepo(tokenRepo)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return handler, jwtMgr, mux, userRepo, groupRepo
}

// ---------------------------------------------------------------------------
// TestAdminUserDisableEnable — PUT /api/admin/users/{id}/active
// ---------------------------------------------------------------------------

func TestAdminUserDisableEnable(t *testing.T) {
	_, jwtMgr, mux, userRepo, _ := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	auth := "Bearer " + tok

	// Seed a user.
	user := &db.User{ID: "u-dis", Username: "carol", PasswordHash: "x", IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	t.Run("disable user", func(t *testing.T) {
		body := `{"active":false}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/active", user.ID),
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("disable: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		// Verify DB state.
		got, err := userRepo.GetByUsername("carol")
		if err != nil || got == nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if got.IsActive {
			t.Error("expected user to be inactive after disable")
		}
	})

	t.Run("enable user", func(t *testing.T) {
		body := `{"active":true}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/active", user.ID),
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", auth)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("enable: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		got, err := userRepo.GetByUsername("carol")
		if err != nil || got == nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if !got.IsActive {
			t.Error("expected user to be active after enable")
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminResetPassword — PUT /api/admin/users/{id}/password
// ---------------------------------------------------------------------------

func TestAdminResetPassword(t *testing.T) {
	_, jwtMgr, mux, userRepo, _ := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	user := &db.User{ID: "u-pw", Username: "dave", PasswordHash: "oldhash", IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	t.Run("reset password succeeds", func(t *testing.T) {
		body := `{"password":"newpass123"}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/password", user.ID),
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		// New password should be verifiable.
		got, _ := userRepo.GetByUsername("dave")
		if got == nil {
			t.Fatal("user not found")
		}
		if got.PasswordHash == "oldhash" {
			t.Error("password hash was not updated")
		}
	})

	t.Run("empty password returns 400", func(t *testing.T) {
		body := `{"password":""}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/password", user.ID),
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminSetUserGroup — PUT /api/admin/users/{id}/group
// ---------------------------------------------------------------------------

func TestAdminSetUserGroup(t *testing.T) {
	_, jwtMgr, mux, userRepo, groupRepo := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	// Create group and user.
	grp := &db.Group{Name: "devs"}
	if err := groupRepo.Create(grp); err != nil {
		t.Fatalf("Create group: %v", err)
	}
	user := &db.User{ID: "u-sg", Username: "eve", PasswordHash: "x", IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	t.Run("assign user to group", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"group_id": grp.ID})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/group", user.ID),
			bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("assign: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		got, _ := userRepo.GetByUsername("eve")
		if got == nil || got.GroupID == nil || *got.GroupID != grp.ID {
			t.Errorf("expected GroupID=%q, got %v", grp.ID, got.GroupID)
		}
	})

	t.Run("ungroup user (group_id null)", func(t *testing.T) {
		body := `{"group_id":null}`
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/group", user.ID),
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("ungroup: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		got, _ := userRepo.GetByUsername("eve")
		if got == nil {
			t.Fatal("user not found")
		}
		if got.GroupID != nil {
			t.Errorf("expected GroupID=nil after ungroup, got %v", *got.GroupID)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminRevokeUserTokens — POST /api/admin/users/{id}/revoke-tokens
// ---------------------------------------------------------------------------

func TestAdminRevokeUserTokens(t *testing.T) {
	_, jwtMgr, mux, userRepo, _ := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	user := &db.User{ID: "u-rt", Username: "frank", PasswordHash: "x", IsActive: true}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	t.Run("revoke tokens returns 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/admin/users/%s/revoke-tokens", user.ID), nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminListUsersByGroup — GET /api/admin/users?group=<name>
// ---------------------------------------------------------------------------

func TestAdminListUsersByGroup(t *testing.T) {
	_, jwtMgr, mux, userRepo, groupRepo := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	grp := &db.Group{Name: "backend"}
	_ = groupRepo.Create(grp)
	u1 := &db.User{ID: "u-lg1", Username: "grace", PasswordHash: "x", IsActive: true}
	u2 := &db.User{ID: "u-lg2", Username: "hank", PasswordHash: "x", IsActive: true}
	_ = userRepo.Create(u1)
	_ = userRepo.Create(u2)
	// Assign u1 to group.
	gid := grp.ID
	_ = userRepo.SetGroup(u1.ID, &gid)

	t.Run("list by group_id returns only group members", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/admin/users?group_id="+grp.ID, nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var users []userResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &users); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(users) != 1 {
			t.Errorf("got %d users, want 1 (only grace in backend group)", len(users))
		}
		if len(users) > 0 && users[0].Username != "grace" {
			t.Errorf("expected grace, got %q", users[0].Username)
		}
	})

	t.Run("list without group filter returns all users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		var users []userResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &users)
		if len(users) < 2 {
			t.Errorf("got %d users, want ≥2 (grace + hank)", len(users))
		}
	})
}
