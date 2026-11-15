package uploadquota

import (
	"errors"
	"math"
	"testing"
)

func fairRecord(id string, user int, request string, staging int64) Reservation {
	return Reservation{ID: id, UserID: user, RequestID: request, BytesKnown: true, Bytes: 1, StagingBytes: staging, CreatedAt: 1000, ExpiresAt: 4600, State: Uploading}
}

func TestStagingFairShares(t *testing.T) {
	for _, tc := range []struct {
		global, user, request, wantUser, wantRequest int64
		invalid                                      bool
	}{
		{1000, 0, 0, 500, 250, false}, {1, 0, 0, 1, 1, false}, {0, 0, 0, 0, 0, false},
		{1000, 800, 700, 800, 700, false}, {1000, 100, 0, 100, 100, false},
		{1000, 1001, 0, 0, 0, true}, {1000, 100, 101, 0, 0, true}, {1000, -1, 0, 0, 0, true},
	} {
		l := Limits{StagingBytes: tc.global, UserStagingBytes: tc.user, RequestStagingBytes: tc.request}
		u, r, e := l.StagingLimits()
		if (e != nil) != tc.invalid || (!tc.invalid && (u != tc.wantUser || r != tc.wantRequest)) {
			t.Fatalf("%+v got %d/%d %v", tc, u, r, e)
		}
	}
}

func TestStagingFairnessCountsAllUnreleased(t *testing.T) {
	limits := durableLimits()
	for _, state := range []State{Reserved, Uploading, Committed, Expired, Aborted} {
		t.Run(string(state), func(t *testing.T) {
			old := fairRecord("old", 1, "a", 240)
			old.State = state
			l := EmptyLedger()
			l.Records[old.ID] = old
			tests := []struct {
				r    Reservation
				want error
			}{
				{fairRecord("new", 1, "a", 11), ErrStaging},  // same request
				{fairRecord("new", 1, "b", 250), nil},        // different request, same owner
				{fairRecord("new", 1, "b", 261), ErrStaging}, // request cap also enforced
				{fairRecord("new", 2, "other", 250), nil},
			}
			for _, tc := range tests {
				if err := checkCapacity(l, nil, tc.r, limits); !errors.Is(err, tc.want) {
					t.Fatalf("%+v got %v want %v", tc.r, err, tc.want)
				}
			}
			// Three requests cannot bypass the owner's aggregate cap.
			l.Records["second"] = fairRecord("second", 1, "b", 240)
			if err := checkCapacity(l, nil, fairRecord("new", 1, "c", 21), limits); !errors.Is(err, ErrStaging) {
				t.Fatal("owner aggregation", err)
			}
			if err := checkCapacity(l, nil, fairRecord("new", 2, "other", 250), limits); err != nil {
				t.Fatal("other owner lost headroom", err)
			}
			old.StagingReleased = true
			l.Records[old.ID] = old
			if err := checkCapacity(l, nil, fairRecord("new", 1, "a", 250), limits); err != nil {
				t.Fatal("confirmed release not reusable", err)
			}
		})
	}
}

func TestStagingGrandfatherDoesNotPermitGrowth(t *testing.T) {
	l := EmptyLedger()
	old := fairRecord("legacy", 1, "a", 800)
	l.Records[old.ID] = old
	limits := durableLimits()
	// Existing over-share record can progress and finish without duplicate charge.
	if err := checkCapacity(l, nil, old, limits); err != nil {
		t.Fatal("legacy completion blocked", err)
	}
	next := old
	next.StagingBytes++
	if err := checkCapacity(l, nil, next, limits); !errors.Is(err, ErrStaging) {
		t.Fatal("legacy growth accepted", err)
	}
	next = old
	next.ID = "new"
	if err := checkCapacity(l, nil, next, limits); !errors.Is(err, ErrStaging) {
		t.Fatal("new ID bypass", err)
	}
	next = old
	next.UserID = 2
	if err := checkCapacity(l, nil, next, limits); !errors.Is(err, ErrStaging) {
		t.Fatal("owner switch bypass", err)
	}
	// First bind of an unbound reservation is not grandfathered.
	old.BytesKnown = false
	old.Bytes = 0
	old.StagingBytes = 0
	l.Records[old.ID] = old
	if err := checkCapacity(l, nil, fairRecord("legacy", 1, "a", 800), limits); !errors.Is(err, ErrStaging) {
		t.Fatal("unbound bypass", err)
	}
	// Cleanup and overflow invariants remain conservative.
	old = fairRecord("legacy", 1, "a", 800)
	old.StagingReleased = true
	l.Records[old.ID] = old
	if err := checkCapacity(l, nil, fairRecord("legacy", 1, "a", 800), limits); !errors.Is(err, ErrStaging) {
		t.Fatal("released bypass", err)
	}
	old.StagingReleased = false
	old.StagingBytes = math.MaxInt64
	l.Records[old.ID] = old
	if err := checkCapacity(l, nil, fairRecord("new", 2, "b", 1), limits); !errors.Is(err, ErrInvalid) {
		t.Fatal("overflow accepted", err)
	}
}

func TestStagingShareDownshiftProgressAndCleanup(t *testing.T) {
	limits := durableLimits()
	limits.UserStagingBytes = 900
	limits.RequestStagingBytes = 900
	upload := fairRecord("legacy", 1, "a", 800)
	l, r, _, err := Apply(EmptyLedger(), nil, Change{Action: Reserve, Upload: upload, Now: 1000, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	limits.UserStagingBytes = 100
	limits.RequestStagingBytes = 100
	l, r, _, err = Apply(l, nil, Change{Action: Progress, Upload: r, Now: 1001, Limits: limits})
	if err != nil {
		t.Fatal("downshift blocked non-growing progress", err)
	}
	_, _, _, err = Apply(l, nil, Change{Action: Reserve, Upload: fairRecord("new", 1, "b", 1), Now: 1001, Limits: limits})
	if !errors.Is(err, ErrStaging) {
		t.Fatal("downshift admitted new work", err)
	}
	_, _, _, err = Apply(l, nil, Change{Action: Abort, Upload: r, Now: 1001, Terminal: Aborted})
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("unconfirmed cleanup accepted", err)
	}
	l, _, _, err = Apply(l, nil, Change{Action: Abort, Upload: r, Now: 1001, Terminal: Aborted, CleanupConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = Apply(l, nil, Change{Action: Reserve, Upload: fairRecord("new", 1, "b", 100), Now: 1001, Limits: limits}); err != nil {
		t.Fatal("confirmed cleanup did not free shares", err)
	}
}
