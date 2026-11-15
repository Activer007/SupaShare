package webserver

import (
	"net/http"

	"github.com/Activer007/SupaShare/internal/configuration"
)

const pageReportOnlyPolicy = "base-uri 'self'; object-src 'none'; script-src 'self' 'wasm-unsafe-eval'"

func addPageResponsePolicy(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Security-Policy-Report-Only", pageReportOnlyPolicy)
	// These default pages were observed with Report-Only first. Script limits
	// remain observational while legacy inline scripts and handlers are used.
	switch name {
	case "login", "download", "error":
		w.Header().Set("Content-Security-Policy", "base-uri 'self'; object-src 'none'")
	}
	// Public download/upload views and the StreamSaver iframe retain their
	// existing embedding behavior; every other page is non-embeddable by default.
	switch name {
	case "download", "download_password", "redirect_filename", "publicUpload", "delivery":
	default:
		policy := w.Header().Get("Content-Security-Policy")
		if policy != "" {
			policy += "; "
		}
		w.Header().Set("Content-Security-Policy", policy+"frame-ancestors 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
	}
}

// withResponsePolicy passes the original writer through so streaming handlers
// retain Flusher, Hijacker and ResponseController support.
func withResponsePolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if configuration.UsesHTTPS() {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
