package migration

import (
	_ "embed"
	"net/http"
	"strings"
)

// dashboardHTML is the single-file control console served at /dashboard. It uses
// only this server's API (GET /migrations, GET /metrics, POST /migrations,
// POST /migrations/{id}/cancel). It contains no hardcoded results.
//
//go:embed dashboard.html
var dashboardHTML []byte

// dashboardAssets holds the vendored browser dependencies the console loads at
// runtime. Three.js is embedded rather than fetched from a CDN so the console
// renders offline and under the Content-Security-Policy served with the page.
//
//go:embed assets/three.module.min.js
var threeModuleJS []byte

//go:embed assets/three.core.min.js
var threeCoreJS []byte

// dashboardAssetTypes maps an embedded asset file name to the Content-Type it is
// served with. Only files listed here can be fetched from /dashboard-assets/.
var dashboardAssetTypes = map[string]string{
	"three.module.min.js": "text/javascript; charset=utf-8",
	"three.core.min.js":   "text/javascript; charset=utf-8",
}

// handleDashboardAsset serves a vendored dashboard dependency, for example
// GET /dashboard-assets/three.module.min.js. Unknown paths return 404.
func (s *Server) handleDashboardAsset(w http.ResponseWriter, r *http.Request, name string) {
	contentType, ok := dashboardAssetTypes[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body []byte
	switch name {
	case "three.module.min.js":
		body = threeModuleJS
	case "three.core.min.js":
		body = threeCoreJS
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// dashboardAssetPath extracts the asset name from a /dashboard-assets/ request
// path. It returns an empty string when the path is the prefix itself.
func dashboardAssetPath(urlPath string) string {
	rest := strings.TrimPrefix(urlPath, dashboardAssetsPrefix)
	rest = strings.Trim(rest, "/")
	if rest == "" || strings.Contains(rest, "/") || strings.Contains(rest, "\\") {
		return ""
	}
	return rest
}
