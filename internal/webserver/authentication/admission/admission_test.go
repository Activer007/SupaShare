package admission

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/logging"
)

func TestAdmissionFairnessExpiryAndBoundedClients(t *testing.T) {
	var l limiter
	now := time.Now()
	for i := 0; i < perClient; i++ {
		if !l.allow(Login, "attacker", now) {
			t.Fatal("premature limit")
		}
	}
	if l.allow(Login, "attacker", now) {
		t.Fatal("client exceeded cap")
	}
	if !l.allow(Login, "other", now) || !l.allow(Logout, "attacker", now) {
		t.Fatal("no fairness/purpose isolation")
	}
	if !l.allow(Login, "attacker", now.Add(window)) {
		t.Fatal("exact expiry not recovered")
	}
	for i := 0; i < globalBudget*2; i++ {
		l.allow(OIDC, fmt.Sprint(i), now)
	}
	if len(l.pools[OIDC].clients) != globalBudget {
		t.Fatal("unbounded limiter")
	}
	if l.allow(OIDC, "another", now) {
		t.Fatal("global cap bypass")
	}
	if !l.allow(OIDC, "another", now.Add(window)) || len(l.pools[OIDC].clients) != 1 {
		t.Fatal("stale clients not reclaimed")
	}
}

func TestAdmissionConcurrentAndProxySpoof(t *testing.T) {
	var l limiter
	now := time.Now()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < perClient*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow(Login, "same", now) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != perClient {
		t.Fatalf("accepted %d", wins.Load())
	}
	r := httptest.NewRequest("GET", "http://localhost/login", nil)
	r.RemoteAddr = "198.51.100.22:1000"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.Header.Set("X-Real-IP", "203.0.113.2")
	r.Header.Set("CF-Connecting-IP", "203.0.113.3")
	if logging.GetIpAddress(r) != "198.51.100.22" {
		t.Fatal("untrusted forwarding headers changed owner")
	}
	w := httptest.NewRecorder()
	Reject(w)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid rejection")
	}
}
