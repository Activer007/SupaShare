//go:build test

package sessionmanager

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestN03CookieScopeAndSelection(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	for _, base := range []string{"http://localhost:53842/", "https://files.example.invalid/"} {
		configuration.Get().ServerUrl = base
		name := sessionCookieName()
		w := httptest.NewRecorder()
		writeSessionCookie(w, "scope-token", time.Now().Add(time.Hour))
		c := w.Result().Cookies()[0]
		if c.Name != name || c.Path != "/" || c.Domain != "" || c.Secure != configuration.UsesHTTPS() || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Fatalf("bad canonical cookie: %+v", c)
		}
		for _, headers := range [][]string{
			{}, {name + "="}, {name + "=one; " + name + "=two"}, {name + "=one", name + "=one"},
		} {
			r := httptest.NewRequest("GET", base+"auth/token", nil)
			for _, h := range headers {
				r.Header.Add("Cookie", h)
			}
			if _, ok := canonicalSessionCookie(r); ok {
				t.Fatalf("accepted ambiguous/missing cookie: %v", headers)
			}
		}
		r := httptest.NewRequest("GET", base, nil)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.AddCookie(&http.Cookie{Name: name, Value: "valid"})
		if c, ok := canonicalSessionCookie(r); !ok || c.Value != "valid" {
			t.Fatal("canonical cookie rejected")
		}
		if configuration.UsesHTTPS() {
			r.AddCookie(&http.Cookie{Name: "session_token", Value: "attacker"})
			if c, ok := canonicalSessionCookie(r); !ok || c.Value != "valid" {
				t.Fatal("legacy shadowed canonical")
			}
			r.Header.Set("Cookie", "session_token=legacy")
			if _, ok := canonicalSessionCookie(r); ok {
				t.Fatal("HTTPS accepted legacy")
			}
		}
	}
}

func TestN03RotationLogoutAndLegacyCleanup(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	for _, base := range []string{"http://localhost:53842/", "https://files.example.invalid/"} {
		configuration.Get().ServerUrl = base
		user := models.User{Id: 9303, Name: "n03-synthetic"}
		database.SaveUser(user, false)
		t.Cleanup(func() { database.DeleteUser(user.Id) })
		predecessor := "n03-old-" + sessionCookieName()
		database.SaveSession(predecessor, models.Session{UserId: user.Id, RenewAt: time.Now().Add(-time.Hour).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
		r := httptest.NewRequest("GET", base+"auth/token", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookieName(), Value: predecessor})
		w := httptest.NewRecorder()
		if _, ok := IsValidSession(w, r, false, 1); !ok {
			t.Fatal("rotation failed")
		}
		var successor *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == sessionCookieName() && c.Value != "" {
				successor = c
			}
		}
		if successor == nil || successor.Path != "/" {
			t.Fatal("rotation not rooted")
		}
		jar, _ := cookiejar.New(nil)
		root, _ := url.Parse(base)
		auth, _ := url.Parse(base + "auth/token")
		jar.SetCookies(root, []*http.Cookie{{Name: "session_token", Value: "legacy-root", Path: "/"}})
		jar.SetCookies(auth, []*http.Cookie{{Name: "session_token", Value: "legacy-auth", Path: "/auth"}})
		jar.SetCookies(auth, w.Result().Cookies())
		for _, c := range jar.Cookies(auth) {
			if c.Value == "legacy-auth" || (configuration.UsesHTTPS() && c.Value == "legacy-root") {
				t.Fatal("legacy cookie survives migration")
			}
		}
		// A predecessor may only revoke its successor, not authenticate again.
		LogoutSession(httptest.NewRecorder(), r)
		if _, ok := database.GetSession(successor.Value); ok {
			t.Fatal("logout did not revoke successor")
		}
	}
}

func TestN03DuplicateLogoutDoesNotPickASession(t *testing.T) {
	old := configuration.Get().ServerUrl
	configuration.Get().ServerUrl = "http://localhost/"
	defer func() { configuration.Get().ServerUrl = old }()
	for _, id := range []string{"n03-first", "n03-second"} {
		database.SaveSession(id, models.Session{UserId: 7, ValidUntil: time.Now().Add(time.Hour).Unix()})
		defer database.DeleteSession(id)
	}
	r := httptest.NewRequest("POST", "http://localhost/logout", nil)
	r.Header.Set("Cookie", "session_token=n03-first; session_token=n03-second")
	if _, ok := IsValidSession(httptest.NewRecorder(), r, false, 1); ok {
		t.Fatal("ambiguous authentication")
	}
	LogoutSession(httptest.NewRecorder(), r)
	for _, id := range []string{"n03-first", "n03-second"} {
		if _, ok := database.GetSession(id); !ok {
			t.Fatal("ambiguous logout revoked arbitrary session")
		}
	}
}

func TestN03LegacyRequestCleanupAndForwardedScheme(t *testing.T) {
	old := configuration.Get().ServerUrl
	defer func() { configuration.Get().ServerUrl = old }()
	for _, base := range []string{"http://localhost/", "https://files.example.invalid/"} {
		configuration.Get().ServerUrl = base
		r := httptest.NewRequest("GET", base+"auth/token", nil)
		r.Header.Set("Cookie", "session_token=one; session_token=two")
		r.Header.Set("X-Forwarded-Proto", "http")
		if !configuration.UsesHTTPS() {
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		w := httptest.NewRecorder()
		clearLegacyRequestCookies(w, r)
		paths := map[string]bool{}
		for _, c := range w.Result().Cookies() {
			if c.Name != "session_token" || c.Domain != "" || c.MaxAge != -1 || c.Secure != configuration.UsesHTTPS() {
				t.Fatalf("invalid legacy cleanup: %+v", c)
			}
			paths[c.Path] = true
		}
		if !paths["/auth"] || paths["/"] != configuration.UsesHTTPS() {
			t.Fatalf("bad cleanup paths: %v", paths)
		}
		canonical := "session_token"
		if configuration.UsesHTTPS() {
			canonical = "__Host-session_token"
		}
		r.Header.Set("Cookie", canonical+"=one")
		if _, ok := canonicalSessionCookie(r); !ok {
			t.Fatal("forwarding header changed canonical selection")
		}
	}
}
