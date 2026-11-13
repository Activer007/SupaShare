package errorHandling

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

func TestErrorEntryPayloadBoundary(t *testing.T) {
	isolateErrorRegistry(t)
	const title = "payload-boundary"
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra-bytes-%d", extra), func(t *testing.T) {
			message := strings.Repeat("x", maxErrorEntryBytes-errorEntryBaseBytes-len(title)-len(WidthDefault)+extra)
			w := httptest.NewRecorder()
			RedirectToErrorPage(w, httptest.NewRequest("GET", "/", nil), title, message, WidthDefault)
			location, err := url.Parse(w.Header().Get("Location"))
			if err != nil || location.Query().Get("e") == "" {
				t.Fatal("error was not registered")
			}
			displayed := Get(httptest.NewRequest("GET", "/error?e="+url.QueryEscape(location.Query().Get("e")), nil))
			if extra == 0 {
				if displayed.Title != title || displayed.Message != message || displayed.CardWidth != WidthDefault {
					t.Fatal("payload at the retained byte limit changed")
				}
			} else if displayed.Title != "error.unknown.title" || displayed.Message != "error.unknownID.help" || displayed.CardWidth != WidthWide {
				t.Fatal("oversized payload did not use the fixed fallback")
			}
		})
	}
}

func TestErrorRegistryReclaimsExpiredEntriesOnInsert(t *testing.T) {
	isolateErrorRegistry(t)
	mutex.Lock()
	for i := 0; i < maxErrorEntries; i++ {
		tokens[fmt.Sprintf("expired-%d", i)] = DisplayedError{expiry: time.Now().Add(-ttl).Unix()}
	}
	mutex.Unlock()
	w := httptest.NewRecorder()
	RedirectToErrorPage(w, httptest.NewRequest("GET", "/", nil), "fresh title", "fresh message", WidthDefault)
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Query().Get("e") == "" {
		t.Fatal("expired entries prevented a fresh error from being registered")
	}
	mutex.RLock()
	count := len(tokens)
	mutex.RUnlock()
	if count != 1 {
		t.Fatalf("entries after expired-capacity reclamation=%d want=1", count)
	}
	displayed := Get(httptest.NewRequest("GET", "/error?e="+url.QueryEscape(location.Query().Get("e")), nil))
	if displayed.Title != "fresh title" || displayed.Message != "fresh message" {
		t.Fatal("fresh error was lost during expired-capacity reclamation")
	}
}

func TestErrorTokenConcurrentReadConsumesExactlyOnce(t *testing.T) {
	isolateErrorRegistry(t)
	w := httptest.NewRecorder()
	RedirectToErrorPage(w, httptest.NewRequest("GET", "/", nil), "one-shot title", "one-shot message", WidthDefault)
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Query().Get("e") == "" {
		t.Fatal("error was not registered")
	}
	target := "/error?e=" + url.QueryEscape(location.Query().Get("e"))
	const readers = 32
	results := make(chan DisplayedError, readers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- Get(httptest.NewRequest("GET", target, nil))
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	consumed := 0
	for displayed := range results {
		if displayed.Title == "one-shot title" && displayed.Message == "one-shot message" {
			consumed++
		} else if displayed.Title != "error.unknownID.title" || displayed.Message != "error.unknownID.help" {
			t.Fatal("concurrent reader received an unexpected error")
		}
	}
	if consumed != 1 {
		t.Fatalf("successful consumers=%d want=1", consumed)
	}
	mutex.RLock()
	count := len(tokens)
	mutex.RUnlock()
	if count != 0 {
		t.Fatal("consumed error remained in the registry")
	}
}
