package migration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardServesPageWithCSP(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("dashboard must send a Content-Security-Policy header")
	}
	body := rec.Body.String()
	for _, want := range []string{"/migrations", "/metrics", "KAIROKUBE CONTROL CONSOLE", "/cancel"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestCancelUnknownMigrationReturns404(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/migrations/MIG-9999/cancel", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCancelRejectsNonPOST(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/migrations/MIG-0001/cancel", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestDashboardRejectsNonGET(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
