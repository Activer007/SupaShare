//go:build test

package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/configupgrade"
	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/environment/flagparser"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestRunOIDCLinkOfflinePreservesExistingAccount(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	cfg := models.DbConnection{HostUrl: filepath.Join(dir, "legacy.sqlite")}
	p, err := dbabstraction.GetNew(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner := models.User{Id: 7, Name: "legacy-owner@example.invalid", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	other := models.User{Id: 8, Name: "ordinary@example.invalid", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
	file := models.File{Id: "legacy-document", Name: "private.txt", UserId: owner.Id, SizeBytes: 3, SHA1: "synthetic-blob"}
	key := models.ApiKey{Id: "synthetic-api-key", UserId: owner.Id}
	p.SaveUser(owner, false)
	p.SaveUser(other, false)
	p.SaveMetaData(file)
	p.SaveApiKey(key)
	p.SaveSession("old-owner-session", models.Session{UserId: owner.Id, ValidUntil: 4102444800})
	p.SaveSession("old-other-session", models.Session{UserId: other.Id, ValidUntil: 4102444800})
	p.SetDbVersion(19)
	p.Close()
	raw, err := sql.Open("sqlite", cfg.HostUrl)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec("DROP TABLE OIDCIdentities")
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}
	settings := models.Configuration{
		ConfigVersion: configupgrade.CurrentConfigVersion,
		DataDir:       data, DatabaseUrl: "sqlite://" + cfg.HostUrl,
		PublicName: "SupaShare", ServerUrl: "http://127.0.0.1/",
		Authentication: models.AuthenticationConfig{
			Method: models.AuthenticationOAuth2, OAuthProvider: "https://idp.example.invalid",
			SaltAdmin: strings.Repeat("a", 30), SaltFiles: strings.Repeat("b", 30),
		},
	}
	if err = os.WriteFile(filepath.Join(dir, "config.json"), settings.ToJson(), 0600); err != nil {
		t.Fatal(err)
	}
	// No real config, issuer requests or user data are used by this command.
	t.Cleanup(configuration.Load)
	t.Setenv("GOKAPI_CONFIG_DIR", dir)
	t.Setenv("GOKAPI_CONFIG_FILE", "config.json")
	t.Setenv("GOKAPI_DATA_DIR", data)
	flags := flagparser.MainFlags{OIDCLink: true, OIDCLinkUser: owner.Id, OIDCSubject: "Synthetic-Case-Sensitive-Subject"}
	if err = runOIDCLink(flags); err != nil {
		t.Fatal(err)
	}
	p, err = dbabstraction.GetNew(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"old-owner-session", "old-other-session"} {
		if _, ok := p.GetSession(session); ok {
			t.Fatal("schema upgrade preserved a legacy browser session")
		}
	}
	p.SaveSession("new-owner-session", models.Session{UserId: owner.Id, ValidUntil: 4102444800})
	p.SaveSession("new-other-session", models.Session{UserId: other.Id, ValidUntil: 4102444800})
	p.Close()
	if err = runOIDCLink(flags); err != nil {
		t.Fatal("repeating the exact operator binding must be safe", err)
	}
	conflict := flags
	conflict.OIDCLinkUser = other.Id
	if err = runOIDCLink(conflict); !errors.Is(err, models.ErrOIDCBindingConflict) {
		t.Fatal("conflicting operator binding accepted", err)
	}
	p, err = dbabstraction.GetNew(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	got, ok, err := p.GetOIDCUser(settings.Authentication.OAuthProvider, flags.OIDCSubject)
	if err != nil || !ok || !reflect.DeepEqual(got, owner) {
		t.Fatalf("operator binding changed the existing owner: %+v, %v", got, err)
	}
	if got, ok := p.GetUser(other.Id); !ok || !reflect.DeepEqual(got, other) {
		t.Fatal("conflict changed the other user", got)
	}
	if got, ok := p.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("operator binding changed file ownership or metadata", got)
	}
	if got, ok := p.GetApiKey(key.Id); !ok || !reflect.DeepEqual(got, key) {
		t.Fatal("operator binding changed API credentials", got)
	}
	if _, ok := p.GetSession("new-owner-session"); ok {
		t.Fatal("repeating the binding did not revoke owner sessions")
	}
	if _, ok := p.GetSession("new-other-session"); !ok {
		t.Fatal("a conflicting binding revoked another user's session")
	}
	log, err := os.ReadFile(filepath.Join(data, "log.txt"))
	if err != nil || !strings.Contains(string(log), "Local operator linked OIDC identity") || strings.Contains(string(log), flags.OIDCSubject) || strings.Contains(string(log), owner.Name) {
		t.Fatal("operator audit missing or exposed subject/email", err)
	}
}
