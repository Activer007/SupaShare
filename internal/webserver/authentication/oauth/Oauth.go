package oauth

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"net/url"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var config oauth2.Config
var ctx context.Context
var provider *oidc.Provider
var issuer string

// Init starts the oauth connection
func Init(baseUrl string, credentials models.AuthenticationConfig) {
	var err error
	ctx = context.Background()
	provider, err = oidc.NewProvider(ctx, credentials.OAuthProvider)
	if err != nil {
		log.Fatal(err)
	}

	issuer = credentials.OAuthProvider
	systemConfig := configuration.Get()
	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	if systemConfig.Authentication.OAuthGroupScope != "" {
		scopes = append(scopes, systemConfig.Authentication.OAuthGroupScope)
	}

	config = oauth2.Config{
		ClientID:     credentials.OAuthClientId,
		ClientSecret: credentials.OAuthClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  baseUrl + "oauth-callback",
		Scopes:       scopes,
	}
}

const (
	promptSilent = iota
	promptSelectAccount
	promptConsent
)

// HandlerLogin is a handler for showing the login screen
func HandlerLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// If user clicked logout, force account selection
	if r.URL.Query().Has("consent") {
		initLogin(w, r, promptConsent)
		return
	}
	initLogin(w, r, promptSilent)
}

func initLogin(w http.ResponseWriter, r *http.Request, screenType int) {
	if !safeCallbackTransport() {
		http.Error(w, "OIDC requires an HTTPS public URL (HTTP is only allowed on loopback for development)", http.StatusServiceUnavailable)
		return
	}
	state, tx, ok := transactions.create()
	if !ok {
		http.Error(w, "Too many pending login attempts; retry later", http.StatusServiceUnavailable)
		return
	}
	setCallbackCookie(w, tx.binding)
	var prompt string
	switch screenType {
	case promptSilent:
		prompt = "none"
	case promptSelectAccount:
		prompt = "select_account"
	case promptConsent:
		prompt = "consent"
	default:
		panic("invalid screen type")
	}
	http.Redirect(w, r, config.AuthCodeURL(state, oauth2.S256ChallengeOption(tx.verifier), oidc.Nonce(tx.nonce), oauth2.SetAuthURLParam("prompt", prompt)), http.StatusFound)
}

func isConsentRequired(r *http.Request) bool {
	return r.URL.Query().Get("error") == "consent_required"
}

func isLoginRequired(r *http.Request) bool {
	errorCode := r.URL.Query().Get("error")
	return errorCode == "login_required" || errorCode == "interaction_required"
}

// HandlerCallback is a handler for processing the oauth callback
func HandlerCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !safeCallbackTransport() {
		http.Error(w, "OIDC requires an HTTPS public URL", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	binding, validCookie := callbackBinding(r)
	states := query["state"]
	if err != nil || !validCookie || len(states) != 1 || len(states[0]) != 32 {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Invalid login transaction", nil)
		return
	}
	tx, valid := transactions.consume(states[0], binding)
	// Clear a matched attempt even when expired or when later processing fails.
	// An unrelated/stale tab must not erase a newer browser login binding.
	if tx.binding != "" {
		setCallbackCookie(w, "")
	}
	if !valid {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Expired or invalid login transaction", nil)
		return
	}

	if isConsentRequired(r) {
		initLogin(w, r, promptConsent)
		return
	}
	if isLoginRequired(r) {
		initLogin(w, r, promptSelectAccount)
		return
	}

	if query.Get("error") != "" || len(query["code"]) != 1 || query.Get("code") == "" {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Authorization was not completed", nil)
		return
	}
	oauth2Token, err := config.Exchange(r.Context(), query.Get("code"), oauth2.VerifierOption(tx.verifier))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		errorHandling.RedirectToOAuthErrorPage(w, r, "Failed to exchange token", err)
		return
	}

	// Bind the identity only after validating the signed ID token against the
	// configured provider and client. UserInfo must describe that same subject.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Missing ID token", nil)
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(r.Context(), rawIDToken)
	if err != nil || idToken.Subject == "" {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Invalid ID token", nil)
		return
	}

	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(tx.nonce)) != 1 {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Invalid ID token nonce", nil)
		return
	}

	userInfo, err := provider.UserInfo(r.Context(), oauth2.StaticTokenSource(oauth2Token))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		errorHandling.RedirectToOAuthErrorPage(w, r, "Failed to get user info", err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if userInfo.Subject != idToken.Subject {
		errorHandling.RedirectToOAuthErrorPage(w, r, "UserInfo subject mismatch", nil)
		return
	}

	if userInfo.Email == "" {
		errorHandling.RedirectToOAuthErrorPage(w, r, "An empty email address was provided.\nPlease make sure that you have your"+
			" email address set in your authentication user backend.", nil)
		return
	}
	info := authentication.OAuthUserInfo{
		Issuer:        issuer,
		Subject:       idToken.Subject,
		EmailVerified: userInfo.EmailVerified,
		Email:         userInfo.Email,
		ClaimsSent:    userInfo,
	}
	err = authentication.CheckOauthUserAndRedirect(w, r, info)
	if err != nil {
		errorHandling.RedirectToOAuthErrorPage(w, r, "Failed to continue with login: ", err)
	}
}
