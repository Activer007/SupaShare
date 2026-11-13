package models

import (
	"errors"
	"unicode/utf8"
)

const (
	// API-key quotas apply to ordinary user keys, not temporary authentication or file-request keys.
	MaxApiKeyNameBytes = 256
	ApiKeyMaxPerUser   = 50
	ApiKeyMaxGlobal    = 10000
)

var ErrApiKeyQuotaExceeded = errors.New("API key quota exceeded; delete unused keys before creating another")
var ErrApiKeyAdmissionBusy = errors.New("API key creation capacity temporarily unavailable")
var ErrInvalidApiKey = errors.New("invalid API key record")

// ValidateNewUserApiKey bounds every variable-width field persisted by this creation path.
// Legacy records remain readable and editable; this does not retroactively revoke credentials.
func ValidateNewUserApiKey(key ApiKey) error {
	if key.UserId <= 0 || key.IsSystemKey || key.UploadRequestId != "" || key.Expiry != 0 ||
		len(key.Id) == 0 || len(key.Id) > 128 || len(key.PublicId) == 0 || len(key.PublicId) > 128 ||
		len(key.FriendlyName) > MaxApiKeyNameBytes || !utf8.ValidString(key.FriendlyName) {
		return ErrInvalidApiKey
	}
	return nil
}
