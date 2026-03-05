package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/l17728/pairproxy/internal/proxy"
)

// ---------------------------------------------------------------------------
// TestAdminDrainUndrain — POST /api/admin/drain, POST /api/admin/undrain,
//                         GET /api/admin/drain/status
// ---------------------------------------------------------------------------

func TestAdminDrainUndrain(t *testing.T) {
	_, jwtMgr, mux, _, _ := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	// Without drain functions configured → 501.
	t.Run("drain not configured returns 501", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/drain", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", rr.Code)
		}
	})

	t.Run("undrain not configured returns 501", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/undrain", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", rr.Code)
		}
	})

	t.Run("drain status not configured returns 501", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/drain/status", nil)
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", rr.Code)
		}
	})
}

func TestAdminDrainWithFunctions(t *testing.T) {
	_, jwtMgr, _, _, _ := setupAdminTestWithTokenRepo(t)

	var draining bool

	// Wire drain functions after initial setup.
	handler, _, _, _, _ := setupAdminTestWithTokenRepo(t)
	tok2 := adminToken(t, jwtMgr)
	authHdr2 := "Bearer " + tok2

	handler.SetDrainFunctions(
		func() error { draining = true; return nil },
		func() error { draining = false; return nil },
		func() proxy.DrainStatus { return proxy.DrainStatus{Draining: draining} },
	)
	mux2 := http.NewServeMux()
	handler.RegisterRoutes(mux2)

	t.Run("drain returns 200 with draining status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/drain", nil)
		req.Header.Set("Authorization", authHdr2)
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["status"] != "draining" {
			t.Errorf("status = %q, want draining", resp["status"])
		}
		if !draining {
			t.Error("drain function was not called")
		}
	})

	t.Run("drain status shows draining=true", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/drain/status", nil)
		req.Header.Set("Authorization", authHdr2)
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		var status proxy.DrainStatus
		_ = json.Unmarshal(rr.Body.Bytes(), &status)
		if !status.Draining {
			t.Error("Draining should be true")
		}
	})

	t.Run("undrain returns 200 with normal status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/undrain", nil)
		req.Header.Set("Authorization", authHdr2)
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["status"] != "normal" {
			t.Errorf("status = %q, want normal", resp["status"])
		}
		if draining {
			t.Error("undrain function should have set draining=false")
		}
	})

	t.Run("drain function error returns 500", func(t *testing.T) {
		handler2, _, _, _, _ := setupAdminTestWithTokenRepo(t)
		handler2.SetDrainFunctions(
			func() error { return errors.New("drain failed") },
			func() error { return nil },
			func() proxy.DrainStatus { return proxy.DrainStatus{} },
		)
		mux3 := http.NewServeMux()
		handler2.RegisterRoutes(mux3)

		tok3 := adminToken(t, jwtMgr)
		req := httptest.NewRequest(http.MethodPost, "/api/admin/drain", nil)
		req.Header.Set("Authorization", "Bearer "+tok3)
		rr := httptest.NewRecorder()
		mux3.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})
}
