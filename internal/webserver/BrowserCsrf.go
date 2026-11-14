package webserver

import (
	"github.com/Activer007/SupaShare/internal/logging"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/admission"
	"net/http"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
)

const browserCSRFLength = 40

func browserCSRFCookieName() string {
	if configuration.UsesHTTPS() {
		return "__Host-gokapi_csrf"
	}
	return "gokapi_csrf"
}

func browserCSRFBinding(r *http.Request) string {
	cookies := r.CookiesNamed(browserCSRFCookieName())
	if len(cookies) != 1 || len(cookies[0].Value) != browserCSRFLength {
		return ""
	}
	for _, c := range cookies[0].Value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return ""
		}
	}
	return cookies[0].Value
}

func newBrowserCSRF(w http.ResponseWriter, r *http.Request, tokenType int) string {
	purpose := admission.Login
	switch tokenType {
	case csrftoken.TypeLogout:
		purpose = admission.Logout
	case csrftoken.TypePasswordChange:
		purpose = admission.PasswordChange
	case csrftoken.TypeLogin:
	default:
		admission.Reject(w)
		return ""
	}
	if !admission.Allow(r, purpose) {
		admission.Reject(w)
		return ""
	}
	binding := browserCSRFBinding(r)
	if binding == "" {
		binding = helper.GenerateRandomString(browserCSRFLength)
	}
	token := csrftoken.GenerateBoundForClient(tokenType, binding, logging.GetIpAddress(r))
	if token == "" {
		admission.Reject(w)
		return ""
	}
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{Name: browserCSRFCookieName(), Value: binding, Path: "/", MaxAge: 1800,
		HttpOnly: true, Secure: configuration.UsesHTTPS(), SameSite: http.SameSiteLaxMode})
	return token
}
