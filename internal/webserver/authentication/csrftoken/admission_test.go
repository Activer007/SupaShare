package csrftoken

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestN04BoundedCSRFAndFairness(t *testing.T) {
	resetStateBlockCleanup()
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if GenerateBoundForClient(TypeLogin, "browser", "ip") != "" {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != maxPerBinding {
		t.Fatalf("binding count %d", wins.Load())
	}
	for i := 0; i < 128; i++ {
		GenerateBoundForClient(TypeLogin, fmt.Sprint(i), "ip")
	}
	count := 0
	for _, v := range tokens {
		if v.Client == "ip" {
			count++
		}
	}
	if count != maxPerClient {
		t.Fatalf("IP count %d", count)
	}
	if GenerateBoundForClient(TypeLogin, "other", "other-ip") == "" || GenerateBoundForClient(TypeLogout, "browser", "ip") == "" {
		t.Fatal("login exhaustion blocked unrelated context")
	}
	for id, v := range tokens {
		v.Expiry = time.Now().Unix()
		tokens[id] = v
	}
	if GenerateBoundForClient(TypeLogin, "browser", "ip") == "" || len(tokens) != 1 {
		t.Fatal("exact expiry not reclaimed on issuance")
	}
}

func TestN04CSRFPurposeCapacityAndSingleUse(t *testing.T) {
	resetStateBlockCleanup()
	for i := 0; i < maxPerPurpose; i++ {
		tokens[fmt.Sprint(i)] = csrfToken{Type: TypeLogin, Expiry: time.Now().Add(ttl).Unix()}
	}
	if GenerateBoundForClient(TypeLogin, "new", "new-ip") != "" {
		t.Fatal("global purpose capacity exceeded")
	}
	token := GenerateBoundForClient(TypeLogout, "browser", "ip")
	if token == "" {
		t.Fatal("logout pool starved")
	}
	if IsValidBound(TypeLogout, token, "wrong") || !IsValidBound(TypeLogout, token, "browser") || IsValidBound(TypeLogout, token, "browser") {
		t.Fatal("binding/one-use changed")
	}
	if Generate(-1) != "" || Generate(TypeLogout+1) != "" {
		t.Fatal("unbounded purposes")
	}
}
