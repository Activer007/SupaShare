package oauth

import (
	"github.com/Activer007/SupaShare/internal/configuration"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestN04TransactionClientCapacityAndRecovery(t *testing.T) {
	s := transactionStore{entries: make(map[string]loginTransaction)}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, ok := s.create("one-ip"); ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != maxTransactionsPerClient {
		t.Fatalf("accepted %d", wins.Load())
	}
	state, tx, ok := s.create("other-ip")
	if !ok {
		t.Fatal("unrelated client starved")
	}
	// Consumption is browser-bound, not IP-bound (mobile clients may change network).
	if _, ok := s.consume(state, tx.binding); !ok {
		t.Fatal("valid callback failed")
	}
	for id, v := range s.entries {
		v.expires = time.Now()
		s.entries[id] = v
	}
	if _, _, ok := s.create("one-ip"); !ok || len(s.entries) != 1 {
		t.Fatal("expiry did not restore capacity")
	}
	if _, _, ok := s.create(""); ok {
		t.Fatal("empty owner bypass")
	}
}

func TestN04LoginCapacityRejectsWithoutReplacingCookie(t *testing.T) {
	oldURL := configuration.Get().ServerUrl
	configuration.Get().ServerUrl = "https://files.example.invalid/"
	defer func() { configuration.Get().ServerUrl = oldURL }()
	r := httptest.NewRequest("GET", "https://files.example.invalid/oauth-login", nil)
	r.RemoteAddr = "203.0.113.202:1234"
	for i := 0; i < maxTransactionsPerClient; i++ {
		if _, _, ok := transactions.create("203.0.113.202"); !ok {
			t.Fatal("fill")
		}
	}
	defer func() {
		transactions.Lock()
		defer transactions.Unlock()
		for id, tx := range transactions.entries {
			if tx.client == "203.0.113.202" {
				delete(transactions.entries, id)
			}
		}
	}()
	w := httptest.NewRecorder()
	HandlerLogin(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" {
		t.Fatal("capacity rejection overwrote cookie or redirected")
	}
}
