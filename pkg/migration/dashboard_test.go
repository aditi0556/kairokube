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
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("dashboard must send a Content-Security-Policy header")
	}
	// The 3D stage loads the vendored Three.js module from this same origin, so
	// script-src must name 'self' alongside 'unsafe-inline'.
	if !strings.Contains(csp, "script-src") || !strings.Contains(csp, "'self'") {
		t.Fatalf("CSP %q must allow same-origin scripts", csp)
	}
	body := rec.Body.String()
	for _, want := range []string{"/migrations", "/metrics", "/health", "/migrations/", "/cancel", "KairoKube", "/dashboard-assets/three.module.min.js", "importmap"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestDashboardAssetServesVendoredThreeJS(t *testing.T) {
	srv := newTestServer(t)
	for _, name := range []string{"three.module.min.js", "three.core.min.js"} {
		req := httptest.NewRequest(http.MethodGet, "/dashboard-assets/"+name, nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /dashboard-assets/%s status = %d, want 200", name, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Fatalf("asset %s content type = %q", name, ct)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("asset %s is empty", name)
		}
	}
}

func TestDashboardAssetRejectsUnknownAndNonGET(t *testing.T) {
	srv := newTestServer(t)

	unknown := httptest.NewRequest(http.MethodGet, "/dashboard-assets/secret.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, unknown)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status = %d, want 404", rec.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "/dashboard-assets/three.module.min.js", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST asset status = %d, want 405", rec.Code)
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
