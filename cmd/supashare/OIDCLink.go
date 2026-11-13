package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/environment/flagparser"
	"github.com/Activer007/SupaShare/internal/logging"
	"github.com/Activer007/SupaShare/internal/models"
)

func validateOIDCLinkFlags(flags flagparser.MainFlags) error {
	if flags.OIDCLinkUser <= 0 || flags.OIDCSubject == "" {
		return errors.New("both --oidc-link-user (positive local user ID) and --oidc-subject are required")
	}
	if flags.Reconfigure || flags.DeploymentPassword != "" || flags.CreateSsl || flags.InstallService || flags.UninstallService || flags.ShowVersion || flags.Migration.DoMigration {
		return errors.New("OIDC linking cannot be combined with another administrative action")
	}
	return nil
}

// runOIDCLink is an operator-only, offline migration command. It never runs setup,
// starts an HTTP listener, or looks up an account by an identity-provider email.
func runOIDCLink(flags flagparser.MainFlags) error {
	if err := validateOIDCLinkFlags(flags); err != nil {
		return err
	}
	if !configuration.Exists() {
		return errors.New("existing configuration required; account linking does not run setup")
	}
	configuration.Load()
	auth := configuration.Get().Authentication
	if auth.Method != models.AuthenticationOAuth2 || auth.OAuthProvider == "" {
		return errors.New("existing OIDC configuration with a provider issuer is required")
	}
	configuration.ConnectDatabase()
	defer database.Close()
	if err := database.BindOIDCIdentity(auth.OAuthProvider, flags.OIDCSubject, flags.OIDCLinkUser); err != nil {
		return err
	}
	// Sessions minted under the former email-only login policy must not survive
	// migration of this account. API credentials are not silently rotated here.
	database.DeleteAllSessionsByUser(flags.OIDCLinkUser)
	encoded, _ := json.Marshal([]string{auth.OAuthProvider, flags.OIDCSubject})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	logging.LogOIDCLink(flags.OIDCLinkUser, fingerprint)
	fmt.Printf("OIDC identity linked to local user #%d; browser sessions revoked. Identity fingerprint: %s\n", flags.OIDCLinkUser, fingerprint)
	return nil
}
