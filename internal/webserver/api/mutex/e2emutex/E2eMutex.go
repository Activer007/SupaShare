// Package e2emutex provides process-local, bounded leases for E2E metadata edits.
package e2emutex

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const autoUnlockDuration = 30 * time.Second
const maxActiveUsers = 1024

var (
	ErrBusy         = errors.New("E2E operation already active")
	ErrCapacity     = errors.New("E2E lease capacity reached")
	ErrInvalidLease = errors.New("invalid or expired E2E lease")
	globalMutex     sync.Mutex
	mutexMap        = make(map[int]*lease)
)

type lease struct {
	token    string
	expires  time.Time
	busy     bool
	released bool
}

// pruneLocked has a fixed upper bound of maxActiveUsers. An active callback
// pins its slot even after expiry/release, so another writer cannot overlap it.
func pruneLocked(now time.Time) {
	for id, l := range mutexMap {
		if !l.busy && (l.released || !now.Before(l.expires)) {
			delete(mutexMap, id)
		}
	}
}

// Acquire returns immediately: there is no per-user waiting queue. Expired idle
// leases are reclaimed lazily; no timers or goroutines survive the request.
func Acquire(ctx context.Context, userID int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if userID <= 0 {
		return "", ErrInvalidLease
	}
	globalMutex.Lock()
	defer globalMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	pruneLocked(time.Now())
	if _, ok := mutexMap[userID]; ok {
		return "", ErrBusy
	}
	if len(mutexMap) >= maxActiveUsers {
		return "", ErrCapacity
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes[:])
	mutexMap[userID] = &lease{token: token, expires: time.Now().Add(autoUnlockDuration)}
	return token, nil
}

func matches(l *lease, token string) bool {
	return l != nil && !l.released && len(token) == 64 && subtle.ConstantTimeCompare([]byte(l.token), []byte(token)) == 1
}

// WithLease validates and exclusively pins a lease for one operation. It never
// holds the global lock during fn. Expiry stops new operations, not an already
// admitted callback; cancellation is checked before entry and is otherwise the
// callback's responsibility. No renewal occurs.
func WithLease(ctx context.Context, userID int, token string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	globalMutex.Lock()
	l := mutexMap[userID]
	if !matches(l, token) || !time.Now().Before(l.expires) || fn == nil {
		if l != nil && !l.busy && !time.Now().Before(l.expires) {
			delete(mutexMap, userID)
		}
		globalMutex.Unlock()
		return ErrInvalidLease
	}
	if l.busy {
		globalMutex.Unlock()
		return ErrBusy
	}
	if err := ctx.Err(); err != nil {
		globalMutex.Unlock()
		return err
	}
	l.busy = true
	globalMutex.Unlock()
	defer func() {
		globalMutex.Lock()
		defer globalMutex.Unlock()
		l.busy = false
		if mutexMap[userID] == l && (l.released || !time.Now().Before(l.expires)) {
			delete(mutexMap, userID)
		}
	}()
	return fn()
}

// Release is token-bound and never allocates. Releasing an in-flight operation
// invalidates the token immediately but retains its slot until fn returns.
func Release(userID int, token string) bool {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	l := mutexMap[userID]
	if !matches(l, token) {
		return false
	}
	l.released = true
	if !l.busy {
		delete(mutexMap, userID)
	}
	return true
}
