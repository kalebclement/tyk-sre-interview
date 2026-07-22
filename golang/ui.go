package main

import (
	_ "embed"
	"net/http"
)

// The dashboard is a single dependency-free HTML file compiled into the binary —
// nothing to mount at runtime, so distroless + readOnlyRootFilesystem stay
// untouched, and no npm build stage enters the supply chain. It renders /healthz
// and /deployments client-side; /metrics stays raw (Grafana is the UI for metrics).
//
//go:embed web/index.html
var indexHTML []byte

// indexHandler serves the embedded dashboard at exactly "/". The "/" mux pattern
// is a catch-all, so any other unregistered path lands here too — return a JSON
// 404 for those, consistent with the API's error shape.
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Message: "not found"})
		return
	}

	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Message: "method not allowed"})
		return
	}

	// Inline-only CSP: the page may not load or call anything external,
	// matching the zero-external-dependency build.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if _, err := w.Write(indexHTML); err != nil {
		// Client went away mid-response; nothing actionable.
		return
	}
}
