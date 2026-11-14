package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"golang.org/x/oauth2"
)

func TestTransactionSingleUseBindingExpiryAndCapacity(t *testing.T) {
	s := transactionStore{entries: make(map[string]loginTransaction)}
	state, tx, ok := s.create()
	if !ok {
		t.Fatal("create")
	}
	if _, ok := s.consume(state, "different-browser"); ok {
		t.Fatal("wrong browser accepted")
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := s.consume(state, tx.binding); ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("consumed %d times", accepted.Load())
	}
	state, tx, _ = s.create()
	tx.expires = time.Now().Add(-time.Second)
	s.entries[state] = tx
	if _, ok := s.consume(state, tx.binding); ok {
		t.Fatal("expired accepted")
	}
	for i := 0; i < maxTransactions; i++ {
		if _, _, ok := s.create(); !ok {
			t.Fatal("premature capacity")
		}
	}
	if _, _, ok := s.create(); ok {
		t.Fatal("unbounded store")
	}
	for key, value := range s.entries {
		value.expires = time.Now().Add(-time.Second)
		s.entries[key] = value
	}
	if _, _, ok := s.create(); !ok {
		t.Fatal("expired records not collected")
	}
}

func TestLoginHostCookiePKCEAndNonce(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	configuration.Get().ServerUrl = "https://files.example.invalid/share/"
	config.Endpoint.AuthURL = "https://idp.invalid/auth"
	config.RedirectURL = "https://files.example.invalid/share/oauth-callback"
	rr := httptest.NewRecorder()
	HandlerLogin(rr, httptest.NewRequest("GET", "/share/oauth-login", nil))
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := loc.Query()
	state := q.Get("state")
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("cookie count")
	}
	c := cookies[0]
	if c.Name != "__Host-supashare-oidc" || c.Path != "/" || c.Domain != "" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe cookie: %+v", c)
	}
	tx, ok := transactions.consume(state, c.Value)
	if !ok {
		t.Fatal("transaction missing")
	}
	if state == c.Value || q.Get("nonce") != tx.nonce || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(tx.verifier) {
		t.Fatal("binding/PKCE/nonce incorrect")
	}
	if strings.Contains(rr.Header().Get("Location"), tx.verifier) {
		t.Fatal("verifier leaked")
	}
}

func TestCallbackRejectsForgedEmptyDuplicateAndReplay(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	configuration.Get().ServerUrl = "https://files.example.invalid/"
	for _, kind := range []string{"forged", "empty", "duplicate-cookie", "duplicate-state", "replay", "wrong-browser", "legacy-cookie"} {
		t.Run(kind, func(t *testing.T) {
			state, tx, _ := transactions.create()
			request := httptest.NewRequest("GET", "/oauth-callback?state="+state+"&error=login_required", nil)
			cookie := &http.Cookie{Name: callbackCookieName(), Value: tx.binding}
			switch kind {
			case "forged":
				request.URL.RawQuery = "state=" + strings.Repeat("a", 32) + "&error=login_required"
				cookie.Value = strings.Repeat("a", 32)
			case "empty":
				request.URL.RawQuery = "state=&error=login_required"
				cookie.Value = ""
			case "duplicate-cookie":
				request.AddCookie(cookie)
			case "duplicate-state":
				request.URL.RawQuery += "&state=" + state
			case "replay":
				transactions.consume(state, tx.binding)
			case "wrong-browser":
				cookie.Value = strings.Repeat("z", 32)
			case "legacy-cookie":
				cookie.Name = "state"
				cookie.Value = state
			}
			request.AddCookie(cookie)
			rr := httptest.NewRecorder()
			HandlerCallback(rr, request)
			if rr.Code != http.StatusTemporaryRedirect || !strings.Contains(rr.Header().Get("Location"), "error") {
				t.Fatal("forged callback did not fail closed")
			}
			for _, c := range rr.Result().Cookies() {
				if c.Name == callbackCookieName() && c.MaxAge != -1 {
					t.Fatal("terminal state cookie retained")
				}
				if c.Name == "session_token" {
					t.Fatal("issued session")
				}
			}
		})
	}
}

func TestCallbackProviderErrorConsumesTransaction(t *testing.T) {
	state, tx, _ := transactions.create()
	req := httptest.NewRequest("GET", "/oauth-callback?state="+state+"&error=access_denied", nil)
	req.AddCookie(&http.Cookie{Name: callbackCookieName(), Value: tx.binding})
	rr := httptest.NewRecorder()
	HandlerCallback(rr, req)
	if _, ok := transactions.consume(state, tx.binding); ok {
		t.Fatal("provider error left reusable transaction")
	}
}

func TestCallbackRejectsNonceMismatch(t *testing.T) {
	for _, nonce := range []string{"", "other-attempt"} {
		mock := newMockOIDCServer()
		initWithMock(mock)
		mock.tokenClaims = map[string]any{"nonce": nonce}
		rr := httptest.NewRecorder()
		HandlerCallback(rr, newRequest("/oauth-callback?state=s&code=validcode", "s"))
		mock.Close()
		if !strings.Contains(rr.Header().Get("Location"), "error") {
			t.Fatal("bad nonce accepted")
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == "session_token" {
				t.Fatal("issued session for bad nonce")
			}
		}
	}
}

func TestOIDCTransportBoundary(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	for _, tc := range []struct {
		url     string
		allowed bool
	}{{"https://files.example.com/app/", true}, {"http://localhost/app/", true}, {"http://127.0.0.1/", true}, {"http://[::1]/", true}, {"http://files.example.com/", false}, {"http://localhost.attacker.invalid/", false}, {"https:///", false}} {
		configuration.Get().ServerUrl = tc.url
		if safeCallbackTransport() != tc.allowed {
			t.Fatalf("wrong policy for %s", tc.url)
		}
		if !tc.allowed {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/oauth-login", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			HandlerLogin(rr, req)
			if rr.Code != 503 {
				t.Fatal("untrusted HTTP login allowed")
			}
		}
	}
}

func TestStaleTabDoesNotClearNewLogin(t *testing.T) {
	oldState, _, _ := transactions.create()
	newState, newTx, _ := transactions.create()
	request := httptest.NewRequest("GET", "/oauth-callback?state="+oldState+"&code=old", nil)
	request.AddCookie(&http.Cookie{Name: callbackCookieName(), Value: newTx.binding})
	rr := httptest.NewRecorder()
	HandlerCallback(rr, request)
	for _, c := range rr.Result().Cookies() {
		if c.Name == callbackCookieName() {
			t.Fatal("stale callback erased new binding")
		}
	}
	if _, ok := transactions.consume(newState, newTx.binding); !ok {
		t.Fatal("new login was invalidated")
	}
}
