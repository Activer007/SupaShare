//go:build test

package setup

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestN03SetupCannotBeFramed(t *testing.T) {
	old := isInitialSetup
	isInitialSetup = true
	defer func() { isInitialSetup = old }()
	w := httptest.NewRecorder()
	handleShowSetup(w, httptest.NewRequest("GET", "/start", nil))
	if !strings.Contains(w.Body.String(), "<html") || w.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("setup response missing enforced frame protection")
	}
}
