//go:build test && !integration

package webserver

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestN03ManagementFramePolicy(t *testing.T) {
	for _, name := range []string{"admin", "uploadreq", "api", "users", "logs", "e2esetup", "changepw", "login", "forgotpw", "index", "error", "future-management-page"} {
		w := httptest.NewRecorder()
		addPageResponsePolicy(w, name)
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s can be framed", name)
		}
	}
	for _, name := range []string{"download", "download_password", "redirect_filename", "publicUpload", "delivery"} {
		w := httptest.NewRecorder()
		addPageResponsePolicy(w, name)
		if strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors") || w.Header().Get("X-Frame-Options") != "" {
			t.Errorf("%s public policy changed", name)
		}
	}
}
