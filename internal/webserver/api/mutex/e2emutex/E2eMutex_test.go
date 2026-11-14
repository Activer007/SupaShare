package e2emutex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func reset(t *testing.T) {
	t.Helper()
	globalMutex.Lock()
	mutexMap = make(map[int]*lease)
	globalMutex.Unlock()
}
func acquire(t *testing.T, id int) string {
	t.Helper()
	s, e := Acquire(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestAcquireSingleWinner(t *testing.T) {
	reset(t)
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := Acquire(context.Background(), 1)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrBusy) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
}
func TestCancellationCapacityAndNoAllocation(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Acquire(ctx, 1); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for i := 1; i <= maxActiveUsers; i++ {
		acquire(t, i)
	}
	if _, e := Acquire(context.Background(), maxActiveUsers+1); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if Release(99999, "invalid") {
		t.Fatal("invalid release")
	}
	if e := WithLease(context.Background(), 99999, "invalid", func() error { return nil }); !errors.Is(e, ErrInvalidLease) {
		t.Fatal(e)
	}
	if len(mutexMap) != maxActiveUsers {
		t.Fatal("validation allocated")
	}
}
func TestExpiryCleanupAndABA(t *testing.T) {
	reset(t)
	synctest.Test(t, func(t *testing.T) {
		old := acquire(t, 1)
		for i := 2; i <= maxActiveUsers; i++ {
			acquire(t, i)
		}
		time.Sleep(autoUnlockDuration)
		fresh := acquire(t, 1)
		if fresh == old || Release(1, old) {
			t.Fatal("old token released new lease")
		}
		if len(mutexMap) != 1 {
			t.Fatal("expired capacity not reclaimed")
		}
		if e := WithLease(context.Background(), 1, old, func() error { t.Fatal("old callback"); return nil }); !errors.Is(e, ErrInvalidLease) {
			t.Fatal(e)
		}
		if !Release(1, fresh) || Release(1, fresh) {
			t.Fatal("release not one-shot")
		}
	})
}
func TestWithLeasePinsPastExpiryAndRelease(t *testing.T) {
	reset(t)
	synctest.Test(t, func(t *testing.T) {
		token := acquire(t, 1)
		entered := make(chan struct{})
		finish := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- WithLease(context.Background(), 1, token, func() error { close(entered); <-finish; return nil })
		}()
		<-entered
		if e := WithLease(context.Background(), 1, token, func() error { t.Error("overlap"); return nil }); !errors.Is(e, ErrBusy) {
			t.Fatal(e)
		}
		time.Sleep(autoUnlockDuration + time.Second)
		if _, e := Acquire(context.Background(), 1); !errors.Is(e, ErrBusy) {
			t.Fatal("expired busy lease reacquired", e)
		}
		if !Release(1, token) {
			t.Fatal("release failed")
		}
		if _, e := Acquire(context.Background(), 1); !errors.Is(e, ErrBusy) {
			t.Fatal("released busy lease reacquired", e)
		}
		// A blocked callback must not monopolize unrelated users.
		other := acquire(t, 2)
		Release(2, other)
		close(finish)
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		fresh := acquire(t, 1)
		if Release(1, token) {
			t.Fatal("ABA release")
		}
		Release(1, fresh)
	})
}
func TestWithLeaseCancellationErrorAndPanic(t *testing.T) {
	reset(t)
	token := acquire(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := WithLease(ctx, 1, token, func() error { t.Fatal("cancelled callback"); return nil }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	want := errors.New("callback failure")
	if e := WithLease(context.Background(), 1, token, func() error { return want }); e != want {
		t.Fatal(e)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing panic")
			}
		}()
		_ = WithLease(context.Background(), 1, token, func() error { panic("synthetic") })
	}()
	if e := WithLease(context.Background(), 1, token, func() error { return nil }); e != nil {
		t.Fatal("pin leaked", e)
	}
	Release(1, token)
}
