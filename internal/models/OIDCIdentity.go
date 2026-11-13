package models

import "errors"

// OIDCIdentity is an administrator-approved, immutable external identity binding.
// Issuer and Subject are case-sensitive; email is deliberately not part of the key.
type OIDCIdentity struct {
	Issuer  string
	Subject string
	UserID  int
}

var ErrOIDCBindingConflict = errors.New("OIDC identity or user is already bound")
var ErrOIDCInvalidIdentity = errors.New("invalid OIDC identity or missing user")

func (i OIDCIdentity) Valid() bool {
	return i.Issuer != "" && len(i.Issuer) <= 2048 && i.Subject != "" && len(i.Subject) <= 255 && i.UserID > 0
}
