package models

import (
	"errors"
	"time"
)

// Outstanding short-lived credentials have a separate budget from permanent API
// keys and public file-request capabilities. Bounds are enforced by the provider.
const TemporaryApiKeyMaxPerUser = 64
const TemporaryApiKeyMaxGlobal = 4096
const TemporaryApiKeyTTL = 5 * time.Minute

var ErrTemporaryApiKeyQuotaExceeded = errors.New("temporary API token limit reached; wait for existing tokens to expire")

func ValidateTemporaryApiKey(key ApiKey, now int64) error {
	if !key.IsSystemKey || key.UploadRequestId != "" || key.UserId <= 0 ||
		key.Expiry <= now || key.Expiry > now+int64(TemporaryApiKeyTTL/time.Second) ||
		len(key.Id) == 0 || len(key.Id) > 128 || len(key.PublicId) == 0 || len(key.PublicId) > 128 ||
		len(key.FriendlyName) > MaxApiKeyNameBytes {
		return ErrInvalidApiKey
	}
	return nil
}
