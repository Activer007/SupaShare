//go:build test && !integration

package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/sessionmanager"
)

func TestLogoutHTTPBoundary(t *testing.T) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	const base = "http://127.0.0.1:53843/"
	session := models.Session{UserId: 1, RenewAt: time.Now().Add(time.Hour).Unix(), ValidUntil: time.Now().Add(24 * time.Hour).Unix()}
	const id = "group-c-logout-boundary"
	database.SaveSession(id, session)
	t.Cleanup(func() { database.DeleteSession(id) })
	call := func(method, path, token, binding, origin, site string, acceptJSON bool) *http.Response {
		t.Helper()
		body := url.Values{"csrf-token": {token}}.Encode()
		r, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		if acceptJSON {
			r.Header.Set("Accept", "application/json")
		}
		r.AddCookie(&http.Cookie{Name: "session_token", Value: id})
		if binding != "" {
			r.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: binding})
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		resp := call(method, "logout", "", "", "", "", false)
		if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
			t.Fatalf("unsafe method %s: %d", method, resp.StatusCode)
		}
		resp.Body.Close()
		if _, ok := database.GetSession(id); !ok {
			t.Fatal("safe GET or rejected method revoked session")
		}
	}
	for _, tc := range []struct {
		method, origin, site string
		status               int
	}{
		{"POST", "", "", http.StatusMethodNotAllowed},
		{"GET", "https://attacker.invalid", "", http.StatusForbidden},
		{"GET", "", "cross-site", http.StatusForbidden},
	} {
		resp := call(tc.method, "logoutToken", "", "", tc.origin, tc.site, false)
		resp.Body.Close()
		if resp.StatusCode != tc.status || len(resp.Cookies()) != 0 {
			t.Fatal("invalid token request was accepted")
		}
	}
	response := call("GET", "logoutToken", "", "", "", "", false)
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	binding := ""
	for _, c := range response.Cookies() {
		if c.Name == browserCSRFCookieName() {
			binding = c.Value
		}
	}
	if payload.Token == "" || binding == "" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("missing fresh token binding")
	}
	if _, ok := database.GetSession(id); !ok {
		t.Fatal("token issuance revoked session")
	}
	for _, test := range []struct{ name, token, binding, origin, site string }{
		{"missing-token", "", binding, "", ""},
		{"wrong-browser", payload.Token, strings.Repeat("x", browserCSRFLength), "", ""},
		{"missing-cookie", payload.Token, "", "", ""},
		{"foreign-origin", payload.Token, binding, "https://attacker.invalid", ""},
		{"cross-site", payload.Token, binding, "", "cross-site"},
		{"same-site-not-origin", payload.Token, binding, "", "same-site"},
		{"wrong-type", csrftoken.GenerateBound(csrftoken.TypeLogin, binding), binding, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := call("POST", "logout", test.token, test.binding, test.origin, test.site, false)
			defer resp.Body.Close()
			if resp.StatusCode != 403 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if _, ok := database.GetSession(id); !ok {
				t.Fatal("rejected POST revoked session")
			}
			for _, c := range resp.Cookies() {
				if c.Name == "session_token" {
					t.Fatal("rejected POST altered cookie")
				}
			}
		})
	}
	resp := call("POST", "logout", payload.Token, binding, "", "same-origin", true)
	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if resp.StatusCode != 200 || result["redirect"] != "login" {
		t.Fatalf("logout failed: %d %v", resp.StatusCode, result)
	}
	if _, ok := database.GetSession(id); ok {
		t.Fatal("logout kept session")
	}
	// A fresh session cannot be revoked with the consumed logout token.
	database.SaveSession(id, session)
	resp = call("POST", "logout", payload.Token, binding, "", "", false)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("logout token replay accepted")
	}
	if _, ok := database.GetSession(id); !ok {
		t.Fatal("replay revoked new session")
	}
}

func TestLogoutHTTPRevokesRotatedCookie(t *testing.T) {
	// Create through the production session manager, then advance its renewal point.
	w := httptest.NewRecorder()
	sessionmanager.CreateSession(w, false, 0, 7)
	old := w.Result().Cookies()[0]
	stored, ok := database.GetSession(old.Value)
	if !ok {
		t.Fatal("created session missing")
	}
	stored.RenewAt = time.Now().Add(-time.Minute).Unix()
	stored.ValidUntil = time.Now().Add(time.Hour).Unix()
	database.SaveSession(old.Value, stored)
	request := httptest.NewRequest("GET", "/admin", nil)
	request.AddCookie(old)
	rotated := httptest.NewRecorder()
	if _, ok := sessionmanager.IsValidSession(rotated, request, false, 0); !ok {
		t.Fatal("rotation failed")
	}
	successor := rotated.Result().Cookies()[0]
	t.Cleanup(func() { database.DeleteSession(old.Value); database.DeleteSession(successor.Value) })
	if successor.Value == old.Value {
		t.Fatal("cookie not rotated")
	}
	// Logout with the victim's stale predecessor, not the thief's successor.
	token := csrftoken.GenerateBound(csrftoken.TypeLogout, sec023LegacyTestBinding)
	body := url.Values{"csrf-token": {token}}.Encode()
	req, _ := http.NewRequest("POST", "http://127.0.0.1:53843/logout", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(old)
	req.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: sec023LegacyTestBinding})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status %d", resp.StatusCode)
	}
	request = httptest.NewRequest("GET", "/admin", nil)
	request.AddCookie(successor)
	if _, ok := sessionmanager.IsValidSession(httptest.NewRecorder(), request, false, 0); ok {
		t.Fatal("stolen successor survived logout")
	}
}

// PR #96 confines reset-required users at requireLogin. Logout must remain
// outside that boundary while retaining CSRF protection on the actual routes.
func TestLogoutHTTPResetRequiredUser(t *testing.T) {
	user := models.User{Id: 9843, Name: "synthetic-reset-logout", UserLevel: models.UserLevelUser, ResetPassword: true}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })
	created := httptest.NewRecorder()
	sessionmanager.CreateSession(created, false, 0, user.Id)
	sessionCookie := created.Result().Cookies()[0]
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, _ := http.NewRequest("GET", "http://127.0.0.1:53843/admin", nil)
	request.AddCookie(sessionCookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || !strings.HasSuffix(response.Header.Get("Location"), "changePassword") {
		t.Fatalf("fixture not confined: %d %s", response.StatusCode, response.Header.Get("Location"))
	}
	request, _ = http.NewRequest("GET", "http://127.0.0.1:53843/logoutToken", nil)
	request.AddCookie(sessionCookie)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Token string `json:"token"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || payload.Token == "" {
		t.Fatalf("reset user cannot get token: status=%d err=%v", response.StatusCode, decodeErr)
	}
	cookies := response.Cookies()
	request, _ = http.NewRequest("POST", "http://127.0.0.1:53843/logout", strings.NewReader(url.Values{"csrf-token": {payload.Token}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(sessionCookie)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	decodeErr = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || result["redirect"] != "login" {
		t.Fatalf("reset user logout failed: %d %v %v", response.StatusCode, result, decodeErr)
	}
	if _, ok := database.GetSession(sessionCookie.Value); ok {
		t.Fatal("reset user session survived")
	}
	stored, ok := database.GetUser(user.Id)
	if !ok || !stored.ResetPassword {
		t.Fatal("logout cleared forced password change")
	}
}
