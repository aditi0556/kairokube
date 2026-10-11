package migration

import _ "embed"

// dashboardHTML is the single-file control console served at /dashboard. It uses
// only this server's API (GET /migrations, GET /metrics, POST /migrations,
// POST /migrations/{id}/cancel). It contains no hardcoded results.
//
//go:embed dashboard.html
var dashboardHTML []byte
