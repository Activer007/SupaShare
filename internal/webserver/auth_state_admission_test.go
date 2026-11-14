//go:build test && !integration

package webserver

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/tokengeneration"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
)

func TestN04BrowserAdmissionDoesNotReplaceCookieOrRenderEmptyToken(t *testing.T) {
	r := httptest.NewRequest("GET", "http://localhost/logoutToken", nil)
	r.RemoteAddr = "203.0.113.201:1234"
	r.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: strings.Repeat("A", browserCSRFLength)})
	var first string
	for i := 0; i < 16; i++ {
		w := httptest.NewRecorder()
		token := newBrowserCSRF(w, r, csrftoken.TypeLogout)
		if token == "" || w.Code != 200 {
			t.Fatal("premature binding rejection")
		}
		if i == 0 {
			first = token
		}
	}
	w := httptest.NewRecorder()
	logoutToken(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal("rejection emitted success/cookie")
	}
	if !csrftoken.IsValidBound(csrftoken.TypeLogout, first, browserCSRFBinding(r)) {
		t.Fatal("rejection invalidated existing token")
	}
	w = httptest.NewRecorder()
	logoutToken(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal("consumption did not release capacity")
	}
	w = httptest.NewRecorder()
	if newBrowserCSRF(w, r, csrftoken.TypeLogin) == "" {
		t.Fatal("logout exhausted login pool")
	}
}

func TestN04LoginAndPasswordCapacityResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		purpose int
		handler http.HandlerFunc
	}{{"login", csrftoken.TypeLogin, showLogin}, {"password", csrftoken.TypePasswordChange, changePassword}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://localhost/"+tc.name, nil)
			r.RemoteAddr = "203.0.113.203:1234"
			r.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: strings.Repeat("B", browserCSRFLength)})
			r = authentication.SetUserInRequest(r, models.User{Id: 7413, ResetPassword: true})
			for i := 0; i < 16; i++ {
				if newBrowserCSRF(httptest.NewRecorder(), r, tc.purpose) == "" {
					t.Fatal("fill")
				}
			}
			w := httptest.NewRecorder()
			tc.handler(w, r)
			if w.Code != http.StatusTooManyRequests || len(w.Result().Cookies()) != 0 || w.Header().Get("Retry-After") == "" || strings.Contains(w.Body.String(), "<form") {
				t.Fatal("capacity emitted empty success form", w.Code, w.Body.String())
			}
		})
	}
}

func TestN04TemporaryTokenHTTPQuota(t *testing.T) {
	user := models.User{Id: 7744, Name: "Synthetic"}
	for i := 0; i < models.TemporaryApiKeyMaxPerUser; i++ {
		if _, _, err := tokengeneration.Generate(user, models.ApiPermEdit); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for id, key := range database.GetAllApiKeys() {
			if key.UserId == user.Id {
				database.DeleteApiKey(id)
			}
		}
	}()
	r := authentication.SetUserInRequest(httptest.NewRequest("GET", "http://localhost/auth/token", nil), user)
	r.Header.Set("permission", "PERM_EDIT")
	w := httptest.NewRecorder()
	handleGenerateAuthToken(w, r)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" || strings.Contains(w.Body.String(), `"key"`) {
		t.Fatal("token cap did not return explicit retry", w.Code, w.Body.String())
	}
}
