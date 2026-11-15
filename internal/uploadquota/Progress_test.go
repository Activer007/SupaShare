package uploadquota

import (
	"errors"
	"testing"
)

func TestPublicProgressAbsoluteCapAndLegacyUnbound(t *testing.T) {
	now := int64(1000)
	r := Reservation{ID: "chunk.synthetic", UserID: 1, RequestID: "request", InitiallyUnbound: true, CreatedAt: now, ExpiresAt: now + MaxLifetime, State: Reserved}
	ledger := EmptyLedger()
	ledger.Records[r.ID] = r
	if _, _, _, err := Apply(ledger, nil, Change{Action: Bind, Upload: Reservation{ID: r.ID, UserID: 1, RequestID: "request", BytesKnown: true, Bytes: 1, StagingBytes: 1}, Now: now + UnboundLifetime, Limits: durableLimits()}); !errors.Is(err, ErrExpired) {
		t.Fatal("legacy empty lease survived", err)
	}
	next, expired, _, err := Apply(ledger, nil, Change{Action: Abort, Upload: r, Now: now + UnboundLifetime, Terminal: Expired, CleanupConfirmed: true})
	if err != nil || expired.ExpiresAt != now+UnboundLifetime {
		t.Fatal(expired, err)
	}
	if _, err := Encode(next); err != nil {
		t.Fatal(err)
	}
	r.BytesKnown = true
	r.Bytes = 1
	r.StagingBytes = 1
	r.State = Uploading
	r.ExpiresAt = now + MaxLifetime - 1
	ledger.Records[r.ID] = r
	_, progress, _, err := Apply(ledger, nil, Change{Action: Progress, Upload: r, Now: r.ExpiresAt - 1, Limits: durableLimits()})
	if err != nil || progress.ExpiresAt != now+MaxLifetime {
		t.Fatal(progress, err)
	}
}

func TestPublicEmptyReceiptRetention(t *testing.T) {
	ledger := EmptyLedger()
	r := Reservation{ID: "chunk.old", UserID: 1, RequestID: "request", InitiallyUnbound: true, CreatedAt: 1000, ExpiresAt: 1300, FinishedAt: 1001, State: Aborted, StagingReleased: true}
	ledger.Records[r.ID] = r
	fresh := Reservation{ID: "chunk.new", UserID: 1, RequestID: "request", ExpiresAt: 1601}
	next, _, _, err := Apply(ledger, nil, Change{Action: Reserve, Upload: fresh, Now: 1301, Limits: durableLimits(), PublicChunkAdmission: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.Records[r.ID]; ok {
		t.Fatal("empty terminal receipt retained all day")
	}
	// Bound or committed receipts retain the old replay window.
	r.BytesKnown = true
	r.Bytes = 1
	r.StagingBytes = 1
	ledger.Records[r.ID] = r
	next, _, _, err = Apply(ledger, nil, Change{Action: Reserve, Upload: fresh, Now: 1301, Limits: durableLimits(), PublicChunkAdmission: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.Records[r.ID]; !ok {
		t.Fatal("bound receipt was removed early")
	}
}
