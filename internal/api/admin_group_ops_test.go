package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/l17728/pairproxy/internal/db"
)

// ---------------------------------------------------------------------------
// TestAdminGroupDelete — DELETE /api/admin/groups/{id}
// ---------------------------------------------------------------------------

func TestAdminGroupDelete(t *testing.T) {
	_, jwtMgr, mux, userRepo, groupRepo := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	t.Run("delete empty group succeeds", func(t *testing.T) {
		grp := &db.Group{Name: "empty-team"}
		if err := groupRepo.Create(grp); err != nil {
			t.Fatalf("Create group: %v", err)
		}
		req := httptest.NewRequest(http.MethodDelete,
			fmt.Sprintf("/api/admin/groups/%s", grp.ID), nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("delete group with members fails (no force)", func(t *testing.T) {
		grp := &db.Group{Name: "occupied-team"}
		if err := groupRepo.Create(grp); err != nil {
			t.Fatalf("Create group: %v", err)
		}
		user := &db.User{ID: "u-del1", Username: "user-in-group", PasswordHash: "x", IsActive: true}
		_ = userRepo.Create(user)
		gid := grp.ID
		_ = userRepo.SetGroup(user.ID, &gid)

		req := httptest.NewRequest(http.MethodDelete,
			fmt.Sprintf("/api/admin/groups/%s", grp.ID), nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409 (group has members, no force); body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("delete group with members force=true unbinds members", func(t *testing.T) {
		grp := &db.Group{Name: "forced-delete-team"}
		if err := groupRepo.Create(grp); err != nil {
			t.Fatalf("Create group: %v", err)
		}
		user := &db.User{ID: "u-del2", Username: "user-in-forced-group", PasswordHash: "x", IsActive: true}
		_ = userRepo.Create(user)
		gid := grp.ID
		_ = userRepo.SetGroup(user.ID, &gid)

		req := httptest.NewRequest(http.MethodDelete,
			fmt.Sprintf("/api/admin/groups/%s?force=true", grp.ID), nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("force delete: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}

		// User should have been ungrouped.
		got, _ := userRepo.GetByUsername("user-in-forced-group")
		if got == nil {
			t.Fatal("user not found after force delete")
		}
		if got.GroupID != nil {
			t.Errorf("expected GroupID=nil after force group delete, got %v", *got.GroupID)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminGroupQuotaAllFields — PUT /api/admin/groups/{id}/quota
// covers monthly, rpm, max_tokens_per_request, concurrent_requests
// ---------------------------------------------------------------------------

func TestAdminGroupQuotaAllFields(t *testing.T) {
	_, jwtMgr, mux, _, groupRepo := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	grp := &db.Group{Name: "quota-all"}
	if err := groupRepo.Create(grp); err != nil {
		t.Fatalf("Create group: %v", err)
	}

	monthly := int64(200000)
	rpm := 30
	maxTokens := int64(4096)
	concurrent := 5

	body, _ := json.Marshal(setQuotaRequest{
		MonthlyTokenLimit:   &monthly,
		RequestsPerMinute:   &rpm,
		MaxTokensPerRequest: &maxTokens,
		ConcurrentRequests:  &concurrent,
	})
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/admin/groups/%s/quota", grp.ID), bytes.NewBuffer(body))
	req.Header.Set("Authorization", authHdr)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}

	// Verify via list groups.
	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/groups", nil)
	req2.Header.Set("Authorization", authHdr)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	var groups []groupResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &groups); err != nil {
		t.Fatalf("unmarshal groups: %v", err)
	}

	var found *groupResponse
	for i := range groups {
		if groups[i].ID == grp.ID {
			found = &groups[i]
			break
		}
	}
	if found == nil {
		t.Fatal("group not found in list")
	}
	if found.MonthlyTokenLimit == nil || *found.MonthlyTokenLimit != monthly {
		t.Errorf("MonthlyTokenLimit = %v, want %d", found.MonthlyTokenLimit, monthly)
	}
	if found.RequestsPerMinute == nil || *found.RequestsPerMinute != rpm {
		t.Errorf("RequestsPerMinute = %v, want %d", found.RequestsPerMinute, rpm)
	}
	if found.MaxTokensPerRequest == nil || *found.MaxTokensPerRequest != maxTokens {
		t.Errorf("MaxTokensPerRequest = %v, want %d", found.MaxTokensPerRequest, maxTokens)
	}
	if found.ConcurrentRequests == nil || *found.ConcurrentRequests != concurrent {
		t.Errorf("ConcurrentRequests = %v, want %d", found.ConcurrentRequests, concurrent)
	}
}
