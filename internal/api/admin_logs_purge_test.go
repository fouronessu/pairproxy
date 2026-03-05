package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// TestAdminPurgeLogs — DELETE /api/admin/logs
// ---------------------------------------------------------------------------

func TestAdminPurgeLogs(t *testing.T) {
	_, jwtMgr, mux, _, _ := setupAdminTestWithTokenRepo(t)
	tok := adminToken(t, jwtMgr)
	authHdr := "Bearer " + tok

	t.Run("purge logs with valid date returns deleted count", func(t *testing.T) {
		body, _ := json.Marshal(purgeLogsRequest{Before: "2020-01-01"})
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/logs", bytes.NewBuffer(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]int64
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// Empty DB → 0 deleted, but response key must exist.
		if _, ok := resp["deleted"]; !ok {
			t.Error(`response must contain "deleted" key`)
		}
	})

	t.Run("purge logs without before param returns 400", func(t *testing.T) {
		body := `{"before":""}`
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/logs", bytes.NewBufferString(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("purge logs with invalid date format returns 400", func(t *testing.T) {
		body := `{"before":"not-a-date"}`
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/logs", bytes.NewBufferString(body))
		req.Header.Set("Authorization", authHdr)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("purge logs requires auth", func(t *testing.T) {
		body := `{"before":"2020-01-01"}`
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/logs", bytes.NewBufferString(body))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
	})
}
