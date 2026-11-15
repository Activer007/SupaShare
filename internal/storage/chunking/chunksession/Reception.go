package chunksession

import (
	"context"
	"sync"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

type receptionKey struct{}
type reception struct {
	store   *Store
	started int64
	ids     map[string]bool
	closed  bool // protected by Store.mu
}

// BeginReceive pins the currently eligible sessions of an authenticated public
// request. File-first clients reveal the UUID only after body reception. Pins do
// not renew durable leases and expire after the receiver's absolute two hours.
// The caller MUST release on every exit and use the progress-bounded receiver.
func (s *Store) BeginReceive(ctx context.Context, userID int, requestID string) (context.Context, func(), error) {
	if ctx == nil || userID <= 0 || requestID == "" {
		return ctx, nil, uploadquota.ErrInvalid
	}
	ledger, err := s.read(ctx)
	if err != nil {
		return ctx, nil, err
	}
	rec := &reception{store: s, started: s.now(), ids: make(map[string]bool)}
	s.mu.Lock()
	if s.receptions == nil {
		s.receptions = make(map[*reception]bool)
	}
	if len(s.receptions) >= 64 {
		s.mu.Unlock()
		return ctx, nil, uploadquota.ErrActive
	}
	s.receptions[rec] = true
	s.mu.Unlock()
	var once sync.Once
	close := func() { once.Do(func() { s.mu.Lock(); rec.closed = true; delete(s.receptions, rec); s.mu.Unlock() }) }
	for id, r := range ledger.Records {
		if r.UserID != userID || r.RequestID != requestID || r.Completing || (r.State != uploadquota.Reserved && r.State != uploadquota.Uploading) || uploadquota.EffectiveExpiry(r) <= rec.started {
			continue
		}
		release, e := s.acquire(ctx, id, true)
		if e != nil {
			close()
			return ctx, nil, e
		}
		s.mu.Lock()
		rec.ids[id] = true
		s.mu.Unlock()
		release()
	}
	if len(rec.ids) == 0 {
		close()
		return ctx, nil, uploadquota.ErrExpired
	}
	return context.WithValue(ctx, receptionKey{}, rec), close, nil
}

func (s *Store) receiving(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for rec := range s.receptions {
		if !rec.closed && rec.ids[id] && s.now()-rec.started < uploadquota.ReceiveLifetime {
			return true
		}
	}
	return false
}

func (s *Store) receiveStarted(ctx context.Context, id string) int64 {
	rec, _ := ctx.Value(receptionKey{}).(*reception)
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec == nil || rec.store != s || rec.closed || !rec.ids[id] || s.now() < rec.started || s.now()-rec.started >= uploadquota.ReceiveLifetime {
		return 0
	}
	return rec.started
}
