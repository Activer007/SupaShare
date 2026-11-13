package logging

import "fmt"

// LogOIDCLink records an explicit local-operator account binding synchronously,
// since the maintenance command exits immediately. No email, subject or token is logged.
func LogOIDCLink(userID int, fingerprint string) {
	createLogEntry(categoryAuth, fmt.Sprintf("Local operator linked OIDC identity %s to user #%d; browser sessions revoked", fingerprint, userID), true)
}
