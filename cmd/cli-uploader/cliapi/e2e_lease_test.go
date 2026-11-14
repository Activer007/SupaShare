package cliapi

import (
	"context"
	"encoding/json"
	"github.com/Activer007/SupaShare/internal/encryption/end2end"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestN11CLILeaseFailures(t *testing.T) {
	for _, mode := range []string{"busy", "bad-json", "get-failed", "decrypt-failed", "set-rejected"} {
		t.Run(mode, func(t *testing.T) {
			oldURL, oldKey := gokapiUrl, e2eKey
			t.Cleanup(func() { gokapiUrl = oldURL; e2eKey = oldKey })
			e2eKey = make([]byte, 32)
			enc, err := end2end.EncryptData(nil, e2eKey)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			unlocks, sets := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.URL.Path {
				case "/e2e/mutex/lock":
					if mode == "busy" {
						w.WriteHeader(409)
						return
					}
					if mode == "bad-json" {
						w.Write([]byte(`{}`))
						return
					}
					json.NewEncoder(w).Encode(map[string]string{"leaseToken": "local-lease"})
				case "/e2e/get":
					if r.Header.Get("e2eLease") != "local-lease" {
						t.Error("get missing lease")
					}
					if mode == "get-failed" {
						w.WriteHeader(500)
						return
					}
					if mode == "decrypt-failed" {
						enc.Content = []byte("invalid")
					}
					json.NewEncoder(w).Encode(enc)
				case "/e2e/set":
					sets++
					if r.Header.Get("e2eLease") != "local-lease" {
						t.Error("set missing lease")
					}
					w.WriteHeader(409)
				case "/e2e/mutex/unlock":
					unlocks++
					if r.Header.Get("e2eLease") != "local-lease" {
						t.Error("unlock missing lease")
					}
					w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			gokapiUrl = server.URL
			if err := addE2EFileInfo(models.E2EFile{Id: "new"}); err == nil {
				t.Fatal("failure accepted")
			}
			mu.Lock()
			defer mu.Unlock()
			want := 1
			if mode == "busy" || mode == "bad-json" {
				want = 0
			}
			if unlocks != want {
				t.Fatalf("unlocks=%d want=%d", unlocks, want)
			}
			if mode != "set-rejected" && sets != 0 {
				t.Fatal("stored after read failure")
			}
		})
	}
}

func TestN11CLIAcquisitionRetryAndCancel(t *testing.T) {
	for _, mode := range []string{"eventual", "busy", "delay-cancel", "fetch-cancel", "fatal"} {
		t.Run(mode, func(t *testing.T) {
			oldURL := gokapiUrl
			defer func() { gokapiUrl = oldURL }()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if mode == "fetch-cancel" {
					<-r.Context().Done()
					return
				}
				if mode == "fatal" {
					w.WriteHeader(500)
					return
				}
				if mode == "eventual" && n == 3 {
					w.Write([]byte(`{"leaseToken":"mine"}`))
					return
				}
				w.WriteHeader(409)
			}))
			defer srv.Close()
			gokapiUrl = srv.URL
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "delay-cancel" || mode == "fetch-cancel" {
				time.AfterFunc(20*time.Millisecond, cancel)
			}
			token, err := acquireE2eLeaseContext(ctx)
			want := int32(1)
			if mode == "eventual" {
				want = 3
				if err != nil || token != "mine" {
					t.Fatalf("token=%q err=%v", token, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if mode == "busy" {
				want = 4
			}
			if calls.Load() != want {
				t.Fatalf("calls=%d want=%d", calls.Load(), want)
			}
		})
	}
}
