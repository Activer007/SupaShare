package oauth

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/helper"
	"golang.org/x/oauth2"
)

const transactionTTL = 10 * time.Minute
const maxTransactions = 4096

type loginTransaction struct {
	binding, verifier, nonce string
	expires                  time.Time
}

type transactionStore struct {
	sync.Mutex
	entries map[string]loginTransaction
}

// Short lived, process-local login attempts. Restarts or callbacks routed to
// another instance fail closed; no authenticated sessions are kept here.
var transactions = transactionStore{entries: make(map[string]loginTransaction)}

func (s *transactionStore) create() (string, loginTransaction, bool) {
	s.Lock()
	defer s.Unlock()
	now := time.Now()
	for key, value := range s.entries {
		if !value.expires.After(now) {
			delete(s.entries, key)
		}
	}
	if len(s.entries) >= maxTransactions {
		return "", loginTransaction{}, false
	}
	state := helper.GenerateRandomString(32)
	tx := loginTransaction{binding: helper.GenerateRandomString(32), verifier: oauth2.GenerateVerifier(), nonce: helper.GenerateRandomString(32), expires: now.Add(transactionTTL)}
	s.entries[state] = tx
	return state, tx, true
}

func (s *transactionStore) consume(state, binding string) (loginTransaction, bool) {
	s.Lock()
	defer s.Unlock()
	tx, ok := s.entries[state]
	if !ok || state == "" || binding == "" {
		return loginTransaction{}, false
	}
	// Do not let an unrelated browser invalidate somebody else's login.
	if subtle.ConstantTimeCompare([]byte(binding), []byte(tx.binding)) != 1 {
		return loginTransaction{}, false
	}
	delete(s.entries, state)
	return tx, tx.expires.After(time.Now())
}

func callbackCookieName() string {
	if configuration.UsesHTTPS() {
		return "__Host-supashare-oidc"
	}
	return "supashare-oidc-dev"
}

func callbackBinding(r *http.Request) (string, bool) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == callbackCookieName() {
			count++
			value = cookie.Value
		}
	}
	return value, count == 1 && len(value) == 32
}

func setCallbackCookie(w http.ResponseWriter, value string) {
	age := int(transactionTTL.Seconds())
	if value == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: callbackCookieName(), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: configuration.UsesHTTPS(), SameSite: http.SameSiteLaxMode})
}

// Only loopback development may use unprefixed cookies. On a shared parent
// domain plain HTTP cannot provide the browser's __Host- isolation guarantee.
func safeCallbackTransport() bool {
	u, err := url.Parse(configuration.Get().ServerUrl)
	if err != nil {
		return false
	}
	if u.Scheme == "https" && u.Hostname() != "" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
