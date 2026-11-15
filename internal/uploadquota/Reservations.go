package uploadquota

import (
	"errors"
	"fmt"

	"github.com/Activer007/SupaShare/internal/models"
)

const (
	LedgerVersion           = 1
	MaxRecords              = 4096
	MaxUserRecords          = 1024
	MaxRequestRecords       = 256
	MaxLedgerBytes          = 4 * 1024 * 1024
	MaxIDLength             = 128
	MaxLifetime       int64 = 24 * 60 * 60
	ReceiptLifetime   int64 = 24 * 60 * 60
	UnboundLifetime   int64 = 5 * 60
	ProgressLifetime  int64 = 15 * 60
	ReserveWindow     int64 = 60
	ReserveBurst            = 6
)

var (
	ErrInvalid = errors.New("invalid upload quota data")
	ErrQuota   = errors.New("upload quota exceeded")
	// These retain ErrQuota identity for callers that only need a capacity
	// decision, while HTTP admission can distinguish temporary backpressure.
	ErrActive     = fmt.Errorf("%w: active upload capacity", ErrQuota)
	ErrStaging    = fmt.Errorf("%w: staging capacity", ErrQuota)
	ErrConflict   = errors.New("upload reservation conflict")
	ErrExpired    = errors.New("upload reservation expired")
	ErrNotFound   = errors.New("upload reservation not found")
	ErrInProgress = errors.New("upload completion already claimed")
	ErrFull       = errors.New("upload receipt capacity reached")
	ErrRetry      = errors.New("upload quota transaction retry limit reached")
	ErrSchema     = errors.New("upload quota schema is unavailable")
)

type State string

const (
	Reserved  State = "reserved"
	Uploading State = "uploading"
	Committed State = "committed"
	Aborted   State = "aborted"
	Expired   State = "expired"
)

// Reservation contains bounded, server-owned identifiers, never filesystem paths
// or credentials. Expired active records retain capacity until cleanup is confirmed.
type Reservation struct {
	ID               string
	UserID           int
	RequestID        string
	BytesKnown       bool
	InitiallyUnbound bool
	Bytes            int64
	StagingBytes     int64
	StagingReleased  bool
	CreatedAt        int64
	ExpiresAt        int64
	FinishedAt       int64
	State            State
	Completing       bool
	FileID           string
	CommitDigest     string
}

type Ledger struct {
	Version int
	Records map[string]Reservation
}

// Limits are literal capacities. Legacy zero-as-unlimited settings must be
// translated by the trusted caller while retaining finite enclosing limits.
type Limits struct {
	User           Usage
	Request        Usage
	Instance       Usage
	UserActive     int64
	RequestActive  int64
	InstanceActive int64
	StagingBytes   int64
	// Zero selects finite shares of StagingBytes, never unlimited.
	UserStagingBytes    int64
	RequestStagingBytes int64
	// Provider overwrites this from the current persisted owner role.
	AdminStagingExempt bool
}

type Action string

const (
	Reserve           Action = "reserve"
	Bind              Action = "bind"
	Progress          Action = "progress"
	Claim             Action = "claim"
	Commit            Action = "commit"
	Abort             Action = "abort"
	ReleaseStaging    Action = "release-staging"
	DuplicateMetadata Action = "duplicate-metadata"
	TransferOwner     Action = "transfer-owner"
	ReplaceMetadata   Action = "replace-metadata"
)

// Change is an internal database command, not a public request parser. Upload
// carries the authenticated scope; Reserve/Bind additionally supply a declaration.
// CleanupConfirmed may only be set after I/O has stopped and staged data is gone.
type Change struct {
	Action             Action
	Upload             Reservation
	Limits             Limits
	Now                int64
	FileID             string
	File               *models.File
	Terminal           State
	CleanupConfirmed   bool
	CheckRequestPolicy bool
	// PublicChunkAdmission enables the server-generated no-body reservation guard.
	PublicChunkAdmission bool
	// ReceiveStartedAt is trusted in-process bounded reception evidence, never client input.
	ReceiveStartedAt int64
	// DetachCollection permits only a collection-to-ordinary metadata duplicate.
	// It does not alter the source or create another File Request charge.
	DetachCollection bool
	// Source is a trusted authorization snapshot, rechecked inside the provider
	// transaction for a metadata duplicate, transfer or replacement. Not persisted.
	Source *models.File
	// Target is the authorized pre-replacement snapshot. DeleteSource expires
	// the source in the same transaction; physical cleanup follows separately.
	Target       *models.File
	DeleteSource bool
}

func EmptyLedger() Ledger {
	return Ledger{Version: LedgerVersion, Records: make(map[string]Reservation)}
}

func (ledger Ledger) HasOutstanding() bool {
	for _, record := range ledger.Records {
		if record.State == Reserved || record.State == Uploading || !record.StagingReleased {
			return true
		}
	}
	return false
}

// EffectiveExpiry also bounds pre-upgrade, no-body File Request reservations.
// Persisted uploading/claimed records retain their established expiry.
func EffectiveExpiry(r Reservation) int64 {
	if r.RequestID != "" && r.InitiallyUnbound && !r.BytesKnown {
		return min(r.ExpiresAt, r.CreatedAt+UnboundLifetime)
	}
	return r.ExpiresAt
}

// ReceiveLifetime is the absolute body reception allowance, not a durable lease.
const ReceiveLifetime int64 = 2 * 60 * 60

// ReceptionValid accepts a previously valid lease only for one already started,
// bounded body reception. It cannot resurrect a terminal record or exceed 24h.
func ReceptionValid(r Reservation, started, now int64) bool {
	return r.RequestID != "" && started >= r.CreatedAt && started < EffectiveExpiry(r) && started <= now && now-started < ReceiveLifetime && now < r.CreatedAt+MaxLifetime
}
