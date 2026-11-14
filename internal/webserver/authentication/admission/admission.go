// Package admission bounds anonymous authentication initiation. Budgets are
// process-local and purpose-isolated; deployments must also limit traffic at ingress.
package admission

import (
	"net/http"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/logging"
)

const (
	Login = iota
	Logout
	PasswordChange
	OIDC
	purposeCount
)
const window = time.Minute
const perClient = 120
const globalBudget = 600

type counter struct {
	until time.Time
	count int
}
type pool struct {
	clients map[string]counter
	global  counter
}
type limiter struct {
	sync.Mutex
	pools [purposeCount]pool
}

var requests limiter

func (l *limiter) allow(purpose int, client string, now time.Time) bool {
	if purpose < 0 || purpose >= purposeCount || client == "" {
		return false
	}
	l.Lock()
	defer l.Unlock()
	p := &l.pools[purpose]
	if p.clients == nil {
		p.clients = make(map[string]counter)
	}
	// No eviction of active clients: a new source cannot reset someone else's budget.
	for key, c := range p.clients {
		if !c.until.After(now) {
			delete(p.clients, key)
		}
	}
	if !p.global.until.After(now) {
		p.global = counter{until: now.Add(window)}
	}
	c := p.clients[client]
	if !c.until.After(now) {
		c = counter{until: now.Add(window)}
	}
	if c.count >= perClient || p.global.count >= globalBudget || (c.count == 0 && len(p.clients) >= globalBudget) {
		return false
	}
	c.count++
	p.global.count++
	p.clients[client] = c
	return true
}

// Allow uses the application's trusted-proxy policy, never raw forwarding headers.
func Allow(r *http.Request, purpose int) bool {
	return requests.allow(purpose, logging.GetIpAddress(r), time.Now())
}

func Reject(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "60")
	http.Error(w, "Too many pending authentication attempts; retry later", http.StatusTooManyRequests)
}
