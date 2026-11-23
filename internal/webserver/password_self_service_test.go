//go:build test && !integration

package webserver

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/i18n"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
)

const passwordTestBaseURL = "http://127.0.0.1:53843"

var passwordTestCSRF = regexp.MustCompile(`name="csrf-token" value="([^"]+)"`)

func newPasswordTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func passwordTestRequest(t *testing.T, client *http.Client, path string, form url.Values, proxyUser string) (*http.Response, string) {
	t.Helper()
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, passwordTestBaseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if proxyUser != "" {
		request.Header.Set("Remote-User", proxyUser)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(data)
}

func passwordFormToken(t *testing.T, body string) string {
	t.Helper()
	match := passwordTestCSRF.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("form did not contain a CSRF token")
	}
	return match[1]
}

func passwordTestLogin(t *testing.T, client *http.Client, name, password string) *http.Response {
	t.Helper()
	response, body := passwordTestRequest(t, client, "/login?lang=en", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login form status: %d", response.StatusCode)
	}
	response, _ = passwordTestRequest(t, client, "/login", url.Values{
		"username": {name}, "password": {password}, "csrf-token": {passwordFormToken(t, body)},
	}, "")
	return response
}

func TestSelfServicePasswordChangeHTTP(t *testing.T) {
	const current = "fixture-previous-password"
	const replacement = "fixture-new-password"
	user := models.User{Id: 9841, Name: "password-change-fixture", UserLevel: models.UserLevelUser,
		Password: configuration.HashPassword(current, false, "")}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })

	first, second := newPasswordTestClient(t), newPasswordTestClient(t)
	response, _ := passwordTestRequest(t, first, "/changePassword", nil, "")
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatal("unauthenticated request did not go to login")
	}
	for _, client := range []*http.Client{first, second} {
		response = passwordTestLogin(t, client, user.Name, current)
		if response.StatusCode != http.StatusTemporaryRedirect || cookieValue(response.Cookies(), "session_token") == "" {
			t.Fatalf("could not create the test login: %d", response.StatusCode)
		}
	}
	response, body := passwordTestRequest(t, first, "/admin", nil, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `href="./changePassword"`) {
		t.Fatal("ordinary account did not get a password change link")
	}
	response, body = passwordTestRequest(t, first, "/changePassword", nil, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `name="currentpw"`) {
		t.Fatal("self-service form did not request the current password")
	}
	token := passwordFormToken(t, body)
	_, body = passwordTestRequest(t, second, "/changePassword", nil, "")
	otherBrowserToken := passwordFormToken(t, body)

	for _, tc := range []struct{ name, old, confirm, csrf, errorKey string }{
		{"wrong browser", current, replacement, otherBrowserToken, "password.incomplete"},
		{"missing current password", "", replacement, "", "password.incomplete"},
		{"incorrect current password", "unmatched-fixture-password", replacement, "", "password.currentIncorrect"},
		{"mismatched new passwords", current, "unmatched-new-password", "", "password.mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			csrf := tc.csrf
			if csrf == "" {
				csrf = token
			}
			response, body := passwordTestRequest(t, first, "/changePassword", url.Values{
				"currentpw": {tc.old}, "newpw": {replacement}, "newpw_c": {tc.confirm}, "csrf-token": {csrf},
			}, "")
			if response.StatusCode != http.StatusOK || !strings.Contains(body, `role="alert">`+i18n.New("en").T(tc.errorKey)+`</p>`) {
				t.Fatalf("expected %s validation error, status %d", tc.errorKey, response.StatusCode)
			}
			stored, ok := database.GetUser(user.Id)
			if !ok || stored.Password != user.Password || stored.ResetPassword || stored.AuthGeneration != user.AuthGeneration {
				t.Fatal("rejected password change modified credentials")
			}
			for _, client := range []*http.Client{first, second} {
				response, _ := passwordTestRequest(t, client, "/admin", nil, "")
				if response.StatusCode != http.StatusOK {
					t.Fatal("rejected password change revoked a login")
				}
			}
			token = passwordFormToken(t, body)
		})
	}
	response, _ = passwordTestRequest(t, first, "/changePassword", url.Values{
		"currentpw": {current}, "newpw": {replacement}, "newpw_c": {replacement}, "csrf-token": {token},
	}, "")
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login?passwordChanged=1" {
		t.Fatalf("password change did not require a fresh login: %d %s", response.StatusCode, response.Header.Get("Location"))
	}
	stored, ok := database.GetUser(user.Id)
	if !ok || stored.ResetPassword || stored.AuthGeneration != user.AuthGeneration+1 {
		t.Fatal("password change did not advance credential state")
	}
	for _, client := range []*http.Client{first, second} {
		response, _ := passwordTestRequest(t, client, "/admin", nil, "")
		if response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") != "/login" {
			t.Fatal("password change left an old login valid")
		}
	}
	response, body = passwordTestRequest(t, first, "/login?passwordChanged=1", nil, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `id="passwordChanged"`) {
		t.Fatal("missing login notice after password change")
	}
	if response := passwordTestLogin(t, first, user.Name, current); response.StatusCode != http.StatusOK || cookieValue(response.Cookies(), "session_token") != "" {
		t.Fatal("old password still signs in")
	}
	if response := passwordTestLogin(t, first, user.Name, replacement); response.StatusCode != http.StatusTemporaryRedirect || cookieValue(response.Cookies(), "session_token") == "" {
		t.Fatal("new password did not sign in")
	}
}

func TestPasswordChangeSuperAdminEntryHTTP(t *testing.T) {
	user, ok := database.GetSuperAdmin()
	if !ok {
		t.Fatal("super admin fixture is missing")
	}
	const sessionID = "fixture-super-admin-password-session"
	database.SaveSession(sessionID, models.Session{UserId: user.Id, AuthGeneration: user.AuthGeneration,
		RenewAt: time.Now().Add(time.Hour).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
	t.Cleanup(func() { database.DeleteSession(sessionID) })
	client := newPasswordTestClient(t)
	baseURL, err := url.Parse(passwordTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(baseURL, []*http.Cookie{{Name: "session_token", Value: sessionID, Path: "/"}})
	response, body := passwordTestRequest(t, client, "/admin", nil, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `href="./changePassword"`) {
		t.Fatal("super admin did not get a password change link")
	}
	response, body = passwordTestRequest(t, client, "/changePassword", nil, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `name="currentpw"`) {
		t.Fatal("super admin did not get the self-service form")
	}
}

func TestPasswordChangeAuthenticationModesHTTP(t *testing.T) {
	user := models.User{Id: 9842, Name: "external-password-fixture", UserLevel: models.UserLevelUser, ResetPassword: true}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })
	const sessionID = "fixture-external-password-session"
	database.SaveSession(sessionID, models.Session{UserId: user.Id, RenewAt: time.Now().Add(time.Hour).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
	baseURL, err := url.Parse(passwordTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	previous := configuration.Get().Authentication
	t.Cleanup(func() {
		configuration.Get().Authentication = previous
		authentication.Init(previous)
	})
	for _, tc := range []struct {
		name, proxyUser string
		method          int
	}{
		{"OIDC", "", models.AuthenticationOAuth2},
		{"trusted header", user.Name, models.AuthenticationHeader},
		{"disabled", "", models.AuthenticationDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := previous
			config.Method = tc.method
			config.HeaderKey = "Remote-User"
			config.OnlyRegisteredUsers = true
			config.OAuthProvider = "https://identity.example.invalid"
			config.OAuthClientId = "fixture-client"
			config.OAuthClientSecret = "fixture-secret"
			config.OAuthRecheckInterval = 1
			configuration.Get().Authentication = config
			authentication.Init(config)
			client := newPasswordTestClient(t)
			if tc.method == models.AuthenticationOAuth2 {
				client.Jar.SetCookies(baseURL, []*http.Cookie{{Name: "session_token", Value: sessionID, Path: "/"}})
			}
			response, body := passwordTestRequest(t, client, "/admin", nil, tc.proxyUser)
			if response.StatusCode != http.StatusOK || strings.Contains(body, `href="./changePassword"`) {
				t.Fatalf("unexpected account menu for %s, status %d", tc.name, response.StatusCode)
			}
			for _, form := range []url.Values{nil, {"newpw": {"fixture-new-password"}}} {
				response, _ = passwordTestRequest(t, client, "/changePassword", form, tc.proxyUser)
				if response.StatusCode != http.StatusForbidden {
					t.Fatalf("local password form not blocked for %s, status %d", tc.name, response.StatusCode)
				}
			}
		})
	}
}
