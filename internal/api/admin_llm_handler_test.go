package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/l17728/pairproxy/internal/auth"
	"github.com/l17728/pairproxy/internal/db"
	"github.com/l17728/pairproxy/internal/proxy"
)

// setupLLMTest creates an AdminHandler wired with LLM binding repo and a mock
// LLM health function.
func setupLLMTest(t *testing.T) (*AdminHandler, *auth.Manager, *http.ServeMux, *db.LLMBindingRepo) {
	t.Helper()
	logger := zaptest.NewLogger(t)

	jwtMgr, err := auth.NewManager(logger, "llm-test-secret")
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
	llmRepo := db.NewLLMBindingRepo(gormDB, logger)

	hash, _ := auth.HashPassword(logger, "adminpass")
	handler := NewAdminHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, hash, time.Hour)
	handler.SetLLMBindingRepo(llmRepo)

	// Mock LLM health function returning two targets.
	handler.SetLLMHealthFn(func() []proxy.LLMTargetStatus {
		return []proxy.LLMTargetStatus{
			{URL: "http://llm1:8080", Name: "LLM-1", Provider: "anthropic", Weight: 1, Healthy: true},
			{URL: "http://llm2:8080", Name: "LLM-2", Provider: "anthropic", Weight: 1, Healthy: false},
		}
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	handler.RegisterLLMRoutes(mux)

	return handler, jwtMgr, mux, llmRepo
}

// ---------------------------------------------------------------------------
// TestAdminLLMTargets — GET /api/admin/llm/targets/status
// ---------------------------------------------------------------------------

func TestAdminLLMTargets(t *testing.T) {
	_, jwtMgr, mux, _ := setupLLMTest(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/targets/status", nil)
	req.Header.Set("Authorization", authHdr)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var targets []llmTargetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &targets); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("got %d targets, want 2", len(targets))
	}
	// First target should be healthy.
	if len(targets) > 0 && !targets[0].Healthy {
		t.Error("targets[0].Healthy should be true")
	}
	// Second target should be unhealthy.
	if len(targets) > 1 && targets[1].Healthy {
		t.Error("targets[1].Healthy should be false")
	}
}

func TestAdminLLMTargets_NotConfigured(t *testing.T) {
	// Create handler WITHOUT SetLLMHealthFn.
	logger := zaptest.NewLogger(t)
	jwtMgr, _ := auth.NewManager(logger, "s")
	gormDB, _ := db.Open(logger, ":memory:")
	_ = db.Migrate(logger, gormDB)
	ctx, cancel := context.WithCancel(context.Background())
	writer := db.NewUsageWriter(gormDB, logger, 100, time.Minute)
	writer.Start(ctx)
	t.Cleanup(func() { cancel(); writer.Wait() })
	userRepo := db.NewUserRepo(gormDB, logger)
	groupRepo := db.NewGroupRepo(gormDB, logger)
	usageRepo := db.NewUsageRepo(gormDB, logger)
	auditRepo := db.NewAuditRepo(logger, gormDB)
	hash, _ := auth.HashPassword(logger, "adminpass")
	handler := NewAdminHandler(logger, jwtMgr, userRepo, groupRepo, usageRepo, auditRepo, hash, time.Hour)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	handler.RegisterLLMRoutes(mux)

	tok := adminToken(t, jwtMgr)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/targets/status", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 when health fn not configured", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// TestAdminLLMBindingsCRUD — bindings list / create / delete
// ---------------------------------------------------------------------------

func TestAdminLLMBindingsCRUD(t *testing.T) {
	_, jwtMgr, mux, _ := setupLLMTest(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	var createdID string

	t.Run("list bindings empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/bindings", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var bindings []llmBindingResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &bindings)
		if bindings == nil {
			t.Error("expected non-nil slice")
		}
	})

	t.Run("create user binding", func(t *testing.T) {
		uid := "bind-user-1"
		body, _ := json.Marshal(createLLMBindingRequest{
			TargetURL: "http://llm1:8080",
			UserID:    &uid,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bindings", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("list bindings after create", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/bindings", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var bindings []llmBindingResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &bindings)
		if len(bindings) == 0 {
			t.Fatal("expected ≥1 binding after create")
		}
		createdID = bindings[0].ID
	})

	t.Run("delete binding", func(t *testing.T) {
		if createdID == "" {
			t.Skip("no binding ID captured")
		}
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/llm/bindings/"+createdID, nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("delete: status = %d, want 204; body: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestAdminLLMBindingCreate_Validation(t *testing.T) {
	_, jwtMgr, mux, _ := setupLLMTest(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	t.Run("missing target_url returns 400", func(t *testing.T) {
		uid := "u1"
		body, _ := json.Marshal(createLLMBindingRequest{UserID: &uid})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bindings", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("missing user_id and group_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(createLLMBindingRequest{TargetURL: "http://llm1:8080"})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bindings", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("both user_id and group_id returns 400", func(t *testing.T) {
		uid, gid := "u1", "g1"
		body, _ := json.Marshal(createLLMBindingRequest{
			TargetURL: "http://llm1:8080",
			UserID:    &uid,
			GroupID:   &gid,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bindings", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminLLMDistribute — POST /api/admin/llm/distribute
// ---------------------------------------------------------------------------

func TestAdminLLMDistribute(t *testing.T) {
	_, jwtMgr, mux, _ := setupLLMTest(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	t.Run("distribute with explicit user_ids and target_urls", func(t *testing.T) {
		body, _ := json.Marshal(llmDistributeRequest{
			UserIDs:    []string{"user-a", "user-b", "user-c"},
			TargetURLs: []string{"http://llm1:8080", "http://llm2:8080"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/distribute", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]int
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["assigned"] != 3 {
			t.Errorf("assigned = %d, want 3", resp["assigned"])
		}
	})

	t.Run("distribute with no targets returns 400", func(t *testing.T) {
		// Override the handler's health fn to return empty for this sub-test:
		// use a test-specific mux with no health fn.
		logger := zaptest.NewLogger(t)
		jwtMgr2, _ := auth.NewManager(logger, "s2")
		gDB, _ := db.Open(logger, ":memory:")
		_ = db.Migrate(logger, gDB)
		ctx, cancel := context.WithCancel(context.Background())
		writer := db.NewUsageWriter(gDB, logger, 100, time.Minute)
		writer.Start(ctx)
		t.Cleanup(func() { cancel(); writer.Wait() })
		uRepo := db.NewUserRepo(gDB, logger)
		gRepo := db.NewGroupRepo(gDB, logger)
		uRepo2 := db.NewUsageRepo(gDB, logger)
		aRepo := db.NewAuditRepo(logger, gDB)
		llmRepo := db.NewLLMBindingRepo(gDB, logger)
		h2, _ := auth.HashPassword(logger, "ap")
		handler2 := NewAdminHandler(logger, jwtMgr2, uRepo, gRepo, uRepo2, aRepo, h2, time.Hour)
		handler2.SetLLMBindingRepo(llmRepo)
		// No health fn → distribute falls back to empty target list
		mux2 := http.NewServeMux()
		handler2.RegisterRoutes(mux2)
		handler2.RegisterLLMRoutes(mux2)

		tok2 := adminToken(t, jwtMgr2)
		body, _ := json.Marshal(llmDistributeRequest{
			UserIDs: []string{"u1"},
			// TargetURLs is empty and no health fn → expect 400
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/distribute", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+tok2)
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 when no targets configured", rr.Code)
		}
	})
}
