//go:build test && !integration

package webserver

import (
	"context"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"testing"
	"time"
)

func TestN16OrdinaryPresignAdmission(t *testing.T) {
	for _, change := range []string{"key-revoked", "consumer-deleted", "issuer-delegation", "consumer-delegation", "grant-expired", "owner", "locator", "encryption", "pending", "expiry", "allowance", "zip-member"} {
		t.Run(change, func(t *testing.T) {
			s := securityfixture.New(t, "")
			s.Seed(presignTestStore{})
			old := configuration.Get().DataDir
			configuration.Get().DataDir = s.DataDir
			t.Cleanup(func() { configuration.Get().DataDir = old })
			owner := s.Users["owner"]
			actor := owner
			key := s.Keys["owner"]
			if change == "issuer-delegation" {
				key = s.Keys["admin"]
			}
			if change == "consumer-delegation" {
				actor = s.Users["admin"]
			}
			grant := models.Presign{UserId: key.UserId, ApiKeyId: key.Id, Expiry: time.Now().Add(time.Minute).Unix()}
			files := []models.File{s.Files["own"]}
			if change == "zip-member" {
				files = append(files, s.Files["protected"])
			}
			current := files[len(files)-1]
			switch change {
			case "key-revoked":
				database.DeleteApiKey(key.Id)
			case "consumer-deleted":
				database.DeleteUser(actor.Id)
			case "issuer-delegation":
				u := s.Users["admin"]
				u.UserLevel = models.UserLevelUser
				u.Permissions = models.UserPermissionNone
				database.SaveUser(u, false)
			case "consumer-delegation":
				actor.UserLevel = models.UserLevelUser
				actor.Permissions = models.UserPermissionNone
				database.SaveUser(actor, false)
			case "grant-expired":
				grant.Expiry = time.Now().Add(-time.Minute).Unix()
			case "owner":
				current.UserId = s.Users["other"].Id
				database.SaveMetaData(current)
			case "locator":
				current.SHA1 = "different-object"
				database.SaveMetaData(current)
			case "encryption":
				current.Encryption.IsEncrypted = true
				database.SaveMetaData(current)
			case "pending", "zip-member":
				current.PendingDeletion = time.Now().Unix()
				database.SaveMetaData(current)
			case "expiry":
				current.UnlimitedTime = false
				current.ExpireAt = time.Now().Add(-time.Minute).Unix()
				database.SaveMetaData(current)
			case "allowance":
				current.UnlimitedDownloads = false
				current.DownloadsRemaining = 0
				database.SaveMetaData(current)
			}
			d, err := storage.PrepareManagementDownload(context.Background(), files, actor.Id, storage.ManagementDownloadOptions{Archive: len(files) > 1, CountOrdinary: true, Validate: presignDownloadValidator(grant, actor.Id, false)})
			if err == nil {
				d.Close()
				t.Fatal("stale/unauthorized snapshot admitted")
			}
			for _, f := range files {
				stored, _ := database.GetMetaDataById(f.Id)
				if stored.DownloadCount != f.DownloadCount {
					t.Fatal("denied archive charged a member")
				}
				if downloadstatus.IsCurrentlyDownloading(f) {
					t.Fatal("denied archive registered member")
				}
			}
		})
	}
}

func TestN16OrdinaryAdmissionPreservesCounterSemantics(t *testing.T) {
	s := securityfixture.New(t, "")
	s.Seed(presignTestStore{})
	old := configuration.Get().DataDir
	configuration.Get().DataDir = s.DataDir
	t.Cleanup(func() { configuration.Get().DataDir = old })
	f := s.Files["own"]
	f.UnlimitedDownloads = false
	f.DownloadsRemaining = 1
	database.SaveMetaData(f)
	grant := models.Presign{UserId: s.Users["owner"].Id, ApiKeyId: s.Keys["owner"].Id, Expiry: time.Now().Add(time.Minute).Unix()}
	for _, count := range []bool{false, true} {
		d, err := storage.PrepareManagementDownload(context.Background(), []models.File{f}, grant.UserId, storage.ManagementDownloadOptions{CountOrdinary: count, Validate: presignDownloadValidator(grant, grant.UserId, false)})
		if err != nil {
			t.Fatal(err)
		}
		if !downloadstatus.IsCurrentlyDownloading(f) {
			t.Fatal("admission did not protect content")
		}
		got, _ := database.GetMetaDataById(f.Id)
		want := 1
		if count {
			want = 0
		}
		if got.DownloadsRemaining != want {
			t.Fatalf("remaining=%d want=%d", got.DownloadsRemaining, want)
		}
		d.Close()
	}
}

func TestN16OrdinaryLegacyUnknownSize(t *testing.T) {
	s := securityfixture.New(t, "")
	s.Seed(presignTestStore{})
	old := configuration.Get().DataDir
	configuration.Get().DataDir = s.DataDir
	t.Cleanup(func() { configuration.Get().DataDir = old })
	f := s.Files["own"]
	f.SizeBytes = 0
	database.SaveMetaData(f)
	grant := models.Presign{UserId: s.Users["owner"].Id, ApiKeyId: s.Keys["owner"].Id, Expiry: time.Now().Add(time.Minute).Unix()}
	d, err := storage.PrepareManagementDownload(context.Background(), []models.File{f}, grant.UserId, storage.ManagementDownloadOptions{Validate: presignDownloadValidator(grant, grant.UserId, false)})
	if err != nil {
		t.Fatalf("legacy ordinary raw download rejected: %v", err)
	}
	d.Close()
}

func TestN16CollectionRetryUsesLeaseDeadline(t *testing.T) {
	s := securityfixture.New(t, "")
	s.Seed(presignTestStore{})
	actor := s.Users["owner"]
	grant := models.Presign{UserId: actor.Id, ApiKeyId: s.Keys["owner"].Id, Expiry: time.Now().Add(-time.Minute).Unix()}
	file := s.Files["own"]
	if presignDownloadValidator(grant, actor.Id, false)(file) {
		t.Fatal("expired initial presign admitted")
	}
	// tryCollectionRetry independently checks the two-minute authenticated lease.
	if !collectionRetryDownloadValidator(grant, actor.Id, time.Now().Add(time.Minute))(file) {
		t.Fatal("valid retry lease incorrectly bounded by consumed presign")
	}
	if collectionRetryDownloadValidator(grant, actor.Id, time.Now().Add(-time.Second))(file) {
		t.Fatal("expired retry lease admitted")
	}
	database.DeleteApiKey(grant.ApiKeyId)
	if presignDownloadValidator(grant, actor.Id, true)(file) {
		t.Fatal("retry ignored issuer revocation")
	}
}
