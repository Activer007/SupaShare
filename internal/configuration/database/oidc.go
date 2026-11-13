package database

import "github.com/Activer007/SupaShare/internal/models"

// GetOIDCUser resolves only an exact provider identity, never an email address.
func GetOIDCUser(issuer, subject string) (models.User, bool, error) {
	return db.GetOIDCUser(issuer, subject)
}

// BindOIDCIdentity is for explicitly approved provisioning, not email-based linking.
func BindOIDCIdentity(issuer, subject string, userID int) error {
	return db.BindOIDCIdentity(issuer, subject, userID)
}

// CreateOIDCUser atomically creates a low-rank account and its provider binding.
func CreateOIDCUser(issuer, subject, name string, permissions models.UserPermission) (models.User, error) {
	return db.CreateOIDCUser(issuer, subject, name, permissions)
}
