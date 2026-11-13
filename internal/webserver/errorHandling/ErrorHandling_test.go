package errorHandling

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func isolateErrorRegistry(t *testing.T) {
	t.Helper()
	mutex.Lock()
	previous := tokens
	tokens = make(map[string]DisplayedError)
	mutex.Unlock()
	t.Cleanup(func() {
		mutex.Lock()
		tokens = previous
		mutex.Unlock()
	})
}

func TestOAuthErrorDoesNotRetainProviderDiagnostics(t *testing.T) {
	isolateErrorRegistry(t)
	diagnostic := strings.Repeat("provider-controlled", 1024)
	r := httptest.NewRequest("GET", "/oauth-callback?error="+url.QueryEscape(diagnostic)+
		"&error_description="+url.QueryEscape(diagnostic), nil)
	w := httptest.NewRecorder()

	RedirectToOAuthErrorPage(w, r, "Parameter state was not provided", fmt.Errorf("%s", diagnostic))
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Query().Get("e") == "" {
		t.Fatal("bounded OAuth error was not registered")
	}
	request := httptest.NewRequest("GET", "/error?e="+url.QueryEscape(location.Query().Get("e")), nil)
	displayed := Get(request)
	if displayed.Title != "error.unknown.title" || displayed.Message != "Parameter state was not provided" ||
		displayed.OAuthProviderMessage != "" {
		t.Fatal("provider-controlled diagnostics reached retained error state")
	}
	if second := Get(request); second.Title != "error.unknownID.title" {
		t.Fatal("displayed error was not consumed on first read")
	}
}

func TestErrorRegistryRefusesExcessEntries(t *testing.T) {
	isolateErrorRegistry(t)
	mutex.Lock()
	for i := 0; i < maxErrorEntries; i++ {
		tokens[fmt.Sprintf("existing-%d", i)] = DisplayedError{expiry: time.Now().Add(ttl).Unix()}
	}
	mutex.Unlock()
	w := httptest.NewRecorder()

	RedirectToErrorPage(w, httptest.NewRequest("GET", "/", nil), "ordinary title", "ordinary message", WidthDefault)
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.RawQuery != "e=" {
		t.Fatal("full registry did not use the non-retaining fallback")
	}
	mutex.RLock()
	defer mutex.RUnlock()
	if len(tokens) != maxErrorEntries {
		t.Fatalf("registry entries=%d want=%d", len(tokens), maxErrorEntries)
	}
}
