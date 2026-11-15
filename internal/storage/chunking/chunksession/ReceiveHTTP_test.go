//go:build test && security_chunk

package chunksession

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/storage/uploadreceive"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// Exercise the actual HTTP receiver and session handoff while only accelerating
// the durable lease clock. No production timeout or lease constant is weakened.
func TestPublicReceiveHTTPLease(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, next := range []bool{false, true} {
			for _, fail := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/next=%t/timeout=%t", backend, next, fail), func(t *testing.T) {
					s := testStore(t, backend)
					var now atomic.Int64
					now.Store(time.Now().Unix())
					s.now = now.Load
					scope := testScopes()[1]
					receipt, err := s.Reserve(context.Background(), scope, testLimits())
					if err != nil {
						t.Fatal(err)
					}
					offset, total := int64(0), int64(3)
					if next {
						receipt, err = testWrite(s, scope, 6, 0, []byte("abc"))
						if err != nil {
							t.Fatal(err)
						}
						offset, total = 3, 6
					}
					pool, _ := uploadreceive.New(4, 64<<20)
					dir := t.TempDir()
					pinned := make(chan struct{})
					done := make(chan error, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						ctx, release, e := s.BeginReceive(r.Context(), scope.UserID, scope.RequestID)
						if e != nil {
							done <- e
							http.Error(w, "pin", 500)
							return
						}
						// An intentionally short request deadline exercises real read timeout
						// cleanup without waiting the production 60-second progress window.
						if fail {
							var cancel context.CancelFunc
							ctx, cancel = context.WithTimeout(ctx, 300*time.Millisecond)
							defer cancel()
						}
						close(pinned)
						u, e := pool.ReceiveScoped(w, r.WithContext(ctx), dir, 1<<20, 0, uploadreceive.Scope{UserID: scope.UserID, RequestID: scope.RequestID})
						if e == nil {
							_, e = s.Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: total, Offset: offset}, u.File, u.Header.Size, 1<<20, testLimits())
							if ce := u.Close(); e == nil {
								e = ce
							}
						}
						release()
						if e != nil {
							w.Header().Set("Connection", "close")
							http.Error(w, "receive failed", 408)
						} else {
							w.WriteHeader(200)
						}
						done <- e
					}))
					defer server.Close()
					var body bytes.Buffer
					form := multipart.NewWriter(&body)
					part, e := form.CreateFormFile("file", "chunk.bin")
					if e != nil {
						t.Fatal(e)
					}
					part.Write([]byte("def"))
					form.Close()
					raw := body.Bytes()
					conn, e := net.Dial("tcp", server.Listener.Addr().String())
					if e != nil {
						t.Fatal(e)
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(3 * time.Second))
					fmt.Fprintf(conn, "POST /chunk HTTP/1.1\r\nHost: test\r\nContent-Type: %s\r\nContent-Length: %s\r\n\r\n", form.FormDataContentType(), strconv.Itoa(len(raw)))
					conn.Write(raw[:len(raw)/2])
					select {
					case <-pinned:
					case <-time.After(time.Second):
						t.Fatal("receive not started")
					}
					// The lease becomes expired while the body is legitimately still arriving.
					now.Store(receipt.ExpiresAt + 1)
					if e = s.Sweep(context.Background()); e != nil {
						t.Fatal(e)
					}
					during := testReceipt(t, s, scope)
					if during.StagingReleased || during.State == uploadquota.Expired {
						t.Fatal("sweep reclaimed active receive", during)
					}
					if !fail {
						conn.Write(raw[len(raw)/2:])
					}
					response, e := http.ReadResponse(bufio.NewReader(conn), nil)
					if e != nil {
						t.Fatal(e)
					}
					response.Body.Close()
					e = <-done
					if fail {
						if !errors.Is(e, uploadreceive.ErrTimeout) || response.StatusCode != 408 {
							t.Fatal("timeout", e, response.StatusCode)
						}
						if e = s.Sweep(context.Background()); e != nil {
							t.Fatal(e)
						}
						after := testReceipt(t, s, scope)
						if after.State != uploadquota.Expired || !after.StagingReleased {
							t.Fatal("failed receive retained pin", after)
						}
					} else {
						if e != nil || response.StatusCode != 200 {
							t.Fatal("legitimate receive", e, response.StatusCode)
						}
						after := testReceipt(t, s, scope)
						if after.ExpiresAt != now.Load()+uploadquota.ProgressLifetime {
							t.Fatal("new progress not renewed", after)
						}
					}
				})
			}
		}
	}
}
