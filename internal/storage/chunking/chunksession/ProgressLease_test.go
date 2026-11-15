//go:build test && security_chunk

package chunksession

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestPublicInitialLeaseAndDemandReclaim(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			scope := testScopes()[1]
			r, err := s.Reserve(context.Background(), scope, testLimits())
			if err != nil || r.ExpiresAt != now+uploadquota.UnboundLifetime {
				t.Fatal(r, err)
			}
			next := scope
			next.ClientID = "synthetic-another-reservation"
			if _, err = s.Reserve(context.Background(), next, testLimits()); !errors.Is(err, uploadquota.ErrActive) {
				t.Fatal("second no-progress admission", err)
			}
			now += uploadquota.UnboundLifetime
			if _, err = testWrite(s, scope, 3, 0, []byte("abc")); !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatal("expired write", err)
			}
			if _, err = s.Reserve(context.Background(), next, testLimits()); err != nil {
				t.Fatal("demand reclaim", err)
			}
			if r = testReceipt(t, s, scope); r.State != uploadquota.Expired || !r.StagingReleased {
				t.Fatal(r)
			}
		})
	}
}
func TestPublicLeaseOnlyNewProgressRenews(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			scope := testScopes()[1]
			initial, err := s.Reserve(context.Background(), scope, testLimits())
			if err != nil {
				t.Fatal(err)
			}
			now += 60
			r, err := testWrite(s, scope, 9, 0, []byte("abc"))
			if err != nil || r.ExpiresAt != now+uploadquota.ProgressLifetime {
				t.Fatal(r, err)
			}
			other := scope
			other.RequestID = "another-request"
			if _, err = testWrite(s, other, 9, 3, []byte("bad")); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("same UUID crossed request scope", err)
			}
			expiry := r.ExpiresAt
			now += 60
			r, err = testWrite(s, scope, 9, 0, []byte("abc"))
			if err != nil || r.ExpiresAt != expiry {
				t.Fatal("duplicate renewed", r, err)
			}
			if _, err = testWrite(s, scope, 9, 0, []byte("bad")); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal(err)
			}
			r, err = testWrite(s, scope, 9, 3, []byte("def"))
			if err != nil || r.ExpiresAt != now+uploadquota.ProgressLifetime {
				t.Fatal(r, err)
			}
			now = r.ExpiresAt
			if _, err = testWrite(s, scope, 9, 6, []byte("ghi")); !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatal("expired progress renewed", err)
			}
			if initial.CreatedAt != r.CreatedAt {
				t.Fatal("absolute anchor changed")
			}
		})
	}
}
func TestPublicLeaseCompletionRestoresProcessingAllowance(t *testing.T) {
	s := testStore(t, "sqlite")
	now := time.Now().Unix()
	s.now = func() int64 { return now }
	scope := testScopes()[1]
	initial, err := s.Reserve(context.Background(), scope, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testWrite(s, scope, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if testReceipt(t, s, scope).ExpiresAt != initial.ExpiresAt {
		t.Fatal("empty chunk renewed")
	}
	l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 0}, "empty-final", testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.CloseInput(); l.Release() }()
	if l.Upload.ExpiresAt != initial.CreatedAt+uploadquota.MaxLifetime {
		t.Fatal("processing allowance", l.Upload)
	}
}
func TestPublicReserveBurstAndConcurrentFairness(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			scope := testScopes()[1]
			var wg sync.WaitGroup
			results := make(chan error, 12)
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					candidate := scope
					candidate.ClientID = fmt.Sprintf("synthetic-concurrent-%d", i)
					_, err := s.Reserve(context.Background(), candidate, testLimits())
					results <- err
				}(i)
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else if !errors.Is(err, uploadquota.ErrActive) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatal(success)
			}
			ledger, err := s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range ledger.Records {
				release, err := s.acquire(context.Background(), r.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				err = s.abort(context.Background(), r, uploadquota.Aborted)
				release()
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 1; i < uploadquota.ReserveBurst; i++ {
				candidate := scope
				candidate.ClientID = fmt.Sprintf("synthetic-burst-%d", i)
				if _, err := s.Reserve(context.Background(), candidate, testLimits()); err != nil {
					t.Fatal(err)
				}
				if err := s.Cancel(context.Background(), candidate); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Reserve(context.Background(), scope, testLimits()); !errors.Is(err, uploadquota.ErrActive) {
				t.Fatal("burst not bounded", err)
			}
			now += uploadquota.ReserveWindow
			if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
				t.Fatal("burst not recovered", err)
			}
		})
	}
}
func TestPublicDemandCleanupFailureRetainsQuota(t *testing.T) {
	s := testStore(t, "sqlite")
	now := time.Now().Unix()
	s.now = func() int64 { return now }
	scope := testScopes()[1]
	if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
		t.Fatal(err)
	}
	// Binding can happen before I/O fails; failed input must not gain a progress lease.
	_, err := s.Write(context.Background(), scope, chunking.ChunkInfo{TotalFilesizeBytes: 3}, errorReader{}, 3, 100, testLimits())
	if err == nil {
		t.Fatal("failed input accepted")
	}
	if testReceipt(t, s, scope).ExpiresAt != now+uploadquota.UnboundLifetime {
		t.Fatal("failed input renewed")
	}
	now += uploadquota.UnboundLifetime
	// A held writer gate must not be waited on or refunded by demand cleanup.
	release, err := s.acquire(context.Background(), ID(scope), false)
	if err != nil {
		t.Fatal(err)
	}
	next := scope
	next.ClientID = "synthetic-next-after-held"
	if _, err := s.Reserve(context.Background(), next, testLimits()); !errors.Is(err, uploadquota.ErrActive) {
		t.Fatal(err)
	}
	if testReceipt(t, s, scope).StagingReleased {
		t.Fatal("held I/O refunded")
	}
	release()
	if _, err := s.Reserve(context.Background(), next, testLimits()); err != nil {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("injected input failure") }

func TestReceptionCannotReviveExpiredOrReplayLease(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			scope := testScopes()[1]
			initial, err := s.Reserve(context.Background(), scope, testLimits())
			if err != nil {
				t.Fatal(err)
			}
			now = initial.ExpiresAt
			if _, _, err = s.BeginReceive(context.Background(), scope.UserID, scope.RequestID); !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatal("late receive revived", err)
			}
			now = initial.CreatedAt + 1
			ctx, release, err := s.BeginReceive(context.Background(), scope.UserID, scope.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			now = initial.ExpiresAt + 1
			r, err := s.Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 3}, bytes.NewReader([]byte("abc")), 3, 1024, testLimits())
			if err != nil {
				t.Fatal(err)
			}
			release()
			ctx, release, err = s.BeginReceive(context.Background(), scope.UserID, scope.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			expiry := r.ExpiresAt
			now = expiry + 1
			r, err = s.Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 3}, bytes.NewReader([]byte("abc")), 3, 1024, testLimits())
			if err != nil || r.ExpiresAt != expiry {
				t.Fatal("duplicate receive renewed", r, err)
			}
			release()
			if _, err = s.Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 3}, bytes.NewReader([]byte("abc")), 3, 1024, testLimits()); !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatal("released context accepted", err)
			}
		})
	}
}

func TestReceiveBusyGateDoesNotQueueOrLeak(t *testing.T) {
	s := testStore(t, "sqlite")
	scope := testScopes()[1]
	if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
		t.Fatal(err)
	}
	unlock, err := s.acquire(context.Background(), ID(scope), false)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, release, err := s.BeginReceive(ctx, scope.UserID, scope.RequestID); !errors.Is(err, uploadquota.ErrInProgress) || release != nil {
		t.Fatal("busy receive did not fail fast", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.receptions) != 0 {
		t.Fatal("failed receive leaked ticket")
	}
}
