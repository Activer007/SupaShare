//go:build test && !integration

package webserver

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/downloadPasswordToken"
	"net/http/httptest"
	"testing"
	"time"
)

func TestN16PasswordTokenIssuanceAfterEdit(t *testing.T) {
	for _, change := range []string{"password", "deleted", "pending", "owner", "locator", "encryption", "expired"} {
		t.Run(change, func(t *testing.T) {
			s := securityfixture.New(t, "already-verified-old-hash")
			s.Seed(presignTestStore{})
			verified := s.Files["protected"]
			defer downloadPasswordToken.DeleteAllForFile(verified.Id)
			unlock := apimutex.LockMany(apimutex.TypeMetaData, verified.Id)
			started := make(chan struct{})
			done := make(chan bool, 1)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/d?id="+verified.Id, nil)
			// Simulates expensive old-password verification already completed. Hold the
			// gate until the edit and token invalidation finish; no timing sleeps.
			go func() { close(started); done <- writeVerifiedFilePwCookie(w, r, verified) }()
			<-started
			current := verified
			switch change {
			case "password":
				current.PasswordHash = "new-hash"
			case "pending":
				current.PendingDeletion = time.Now().Unix()
			case "owner":
				current.UserId++
			case "locator":
				current.SHA1 = "replacement"
			case "encryption":
				current.Encryption = models.EncryptionInfo{IsEncrypted: true}
			case "expired":
				current.UnlimitedTime = false
				current.ExpireAt = time.Now().Add(-time.Minute).Unix()
			}
			if change == "deleted" {
				database.DeleteMetaData(current.Id)
			} else {
				database.SaveMetaData(current)
			}
			downloadPasswordToken.DeleteAllForFile(current.Id)
			unlock()
			select {
			case issued := <-done:
				if issued || len(w.Result().Cookies()) != 0 {
					t.Fatal("stale verification minted a token")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("issuance deadlocked")
			}
		})
	}
}

func TestN16PasswordTokenIssuanceAndLaterRevocation(t *testing.T) {
	s := securityfixture.New(t, "verified-current-hash")
	s.Seed(presignTestStore{})
	f := s.Files["protected"]
	defer downloadPasswordToken.DeleteAllForFile(f.Id)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/d", nil)
	if !writeVerifiedFilePwCookie(w, r, f) {
		t.Fatal("current password rejected")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	check := httptest.NewRequest("GET", "/downloadFile", nil)
	check.AddCookie(cookies[0])
	if !isValidPwCookie(check, f) {
		t.Fatal("issued cookie unusable")
	}
	unlock := apimutex.LockMany(apimutex.TypeMetaData, f.Id)
	f.PasswordHash = "replacement-password"
	database.SaveMetaData(f)
	downloadPasswordToken.DeleteAllForFile(f.Id)
	unlock()
	if isValidPwCookie(check, f) {
		t.Fatal("later edit failed to revoke cookie")
	}
}
