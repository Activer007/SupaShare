package webserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
)

// logoutToken issues a fresh, short-lived, one-use token on demand. This avoids
// stale tokens in long-lived admin tabs and also works with a rotated cookie.
func logoutToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !logoutSameOrigin(r) {
		http.Error(w, "Cross-origin token request rejected", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": newBrowserCSRF(w, r, csrftoken.TypeLogout)})
}

// The configured public origin remains correct behind a reverse proxy. Missing
// browser metadata is allowed for older clients, but POST still requires the
// unpredictable browser-bound token. Explicit cross-origin metadata fails closed.
func logoutSameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	actual, err := url.Parse(origin)
	if err != nil || actual.User != nil || actual.Host == "" {
		return false
	}
	expected, err := url.Parse(configuration.Get().ServerUrl)
	return err == nil && strings.EqualFold(actual.Scheme, expected.Scheme) && strings.EqualFold(actual.Host, expected.Host)
}
