package sqlite

import (
	"github.com/Activer007/SupaShare/internal/models"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthGenerationInterleavings(t *testing.T) {
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "generation.sqlite")}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Independent pool/connection models another server process; no Go mutex is shared.
	q, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	original := models.User{Id: 91, Name: "generation-user", Password: "legacy-hash", UserLevel: models.UserLevelUser}
	p.SaveUser(original, false)
	session := models.Session{UserId: original.Id, ValidUntil: time.Now().Add(time.Hour).Unix()}
	if ok, err := p.SaveSessionIfCredentials("before-reset", session, original); err != nil || !ok {
		t.Fatalf("pre-reset issuance: %v %v", ok, err)
	}
	// Pause a login after password verification, then complete reset on the other connection.
	verified := make(chan models.User)
	resume := make(chan struct{})
	result := make(chan bool)
	go func() {
		snapshot, _ := p.GetUser(original.Id)
		verified <- snapshot
		<-resume
		ok, err := p.SaveSessionIfCredentials("late-login", session, snapshot)
		result <- ok || err != nil
	}()
	<-verified
	if ok, err := q.UpdateUserResetCredentials(original, "temporary-hash", true); err != nil || !ok {
		t.Fatalf("reset: %v %v", ok, err)
	}
	close(resume)
	if <-result {
		t.Fatal("old-password session was issued after reset")
	}
	if _, ok := p.GetSession("late-login"); ok {
		t.Fatal("rejected issuance persisted session")
	}
	current, _ := q.GetUser(original.Id)
	prior, _ := p.GetSession("before-reset")
	if current.AuthGeneration != original.AuthGeneration+1 || prior.AuthGeneration == current.AuthGeneration {
		t.Fatal("reset did not invalidate pre-existing sessions by generation")
	}
	if ok, err := p.RehashPassword(original, "old-argon-hash"); err != nil || ok {
		t.Fatalf("stale rehash: %v %v", ok, err)
	}
	if ok, err := p.UpdateUserResetCredentials(original, "attacker-chosen", false); err != nil || ok {
		t.Fatalf("stale self-service reset: %v %v", ok, err)
	}
	if ok, err := p.UpdateUserProfile(original, original); err != nil || ok {
		t.Fatalf("stale profile: %v %v", ok, err)
	}
	got, _ := q.GetUser(original.Id)
	if got != current {
		t.Fatal("stale profile rolled back reset")
	}
	// Even unchanged hash and already-true reset flag MUST advance the generation.
	if ok, err := q.UpdateUserResetCredentials(current, current.Password, current.ResetPassword); err != nil || !ok {
		t.Fatalf("same-password reset: %v %v", ok, err)
	}
	got, _ = p.GetUser(original.Id)
	if got.AuthGeneration != current.AuthGeneration+1 {
		t.Fatal("same-password reset did not advance generation")
	}
	session.AuthGeneration = current.AuthGeneration
	if ok, err := p.SaveSessionIfCredentials("same-hash-stale", session, current); err != nil || ok {
		t.Fatalf("same-hash stale issuance: %v %v", ok, err)
	}
	// Fresh successful migration only changes the verified hash, never profile fields or epoch.
	current = got
	renamed := current
	renamed.Name = "renamed"
	renamed.LastOnline = 123
	q.SaveUser(renamed, false)
	if ok, err := p.RehashPassword(current, "upgraded-hash"); err != nil || !ok {
		t.Fatalf("fresh rehash: %v %v", ok, err)
	}
	got, _ = q.GetUser(original.Id)
	if got.AuthGeneration != current.AuthGeneration || got.Name != "renamed" || got.LastOnline != 123 || got.Password != "upgraded-hash" {
		t.Fatal("rehash changed unrelated data")
	}
	session.AuthGeneration = got.AuthGeneration
	if ok, err := p.SaveSessionIfCredentials("fresh", session, got); err != nil || !ok {
		t.Fatalf("fresh issuance: %v %v", ok, err)
	}
	persisted, _ := q.GetSession("fresh")
	if persisted != session {
		t.Fatalf("session generation roundtrip: %#v != %#v", persisted, session)
	}
	q.DeleteUser(original.Id)
	if ok, err := p.SaveSessionIfCredentials("deleted", session, got); err != nil || ok {
		t.Fatalf("deleted-user issuance: %v %v", ok, err)
	}
	if ok, err := p.RehashPassword(got, "must-not-recreate"); err != nil || ok {
		t.Fatalf("deleted-user rehash: %v %v", ok, err)
	}
	if _, ok := p.GetUser(original.Id); ok {
		t.Fatal("deleted user recreated")
	}
}

func TestAuthGenerationUpgrade(t *testing.T) {
	p, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "upgrade.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	original := models.User{Id: 81, Name: "upgrade-user", Password: "unchanged"}
	p.SaveUser(original, false)
	p.SaveSession("legacy", models.Session{UserId: 81, ValidUntil: time.Now().Add(time.Hour).Unix()})
	if err := p.rawSqlite("ALTER TABLE Users DROP COLUMN AuthGeneration; ALTER TABLE Sessions DROP COLUMN AuthGeneration"); err != nil {
		t.Fatal(err)
	}
	before := p.GetAllUsers()
	if len(before) != 1 || before[0] != original {
		t.Fatal("legacy offline migration read failed")
	}
	p.Upgrade(21)
	p.Upgrade(21) // Retry-safe if interrupted before user_version update.
	got, ok := p.GetUser(81)
	if !ok || got != original {
		t.Fatalf("upgrade user: %#v", got)
	}
	if _, ok := p.GetSession("legacy"); ok {
		t.Fatal("legacy session survived migration")
	}
}

func TestUpdateUserProfileCredentialsIsolation(t *testing.T) {
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "profile.sqlite")}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	original := models.User{Id: 77, Name: "profile-user", Password: "hash", UserLevel: models.UserLevelUser, LastOnline: 100}
	p.SaveUser(original, false)
	update := original
	update.Name = "profile-renamed"
	update.Password = "must-not-change"
	update.ResetPassword = true
	update.AuthGeneration = 1000
	update.LastOnline = 0
	if ok, err := p.UpdateUserProfile(original, update); err != nil || !ok {
		t.Fatalf("profile update: %v %v", ok, err)
	}
	got, _ := p.GetUser(original.Id)
	want := original
	want.Name = update.Name
	if got != want {
		t.Fatalf("profile mutated credentials/usage: %#v", got)
	}
	if ok, err := p.RehashPassword(got, "new-hash"); err != nil || !ok {
		t.Fatalf("rehash: %v %v", ok, err)
	}
	if ok, err := p.UpdateUserProfile(got, update); err != nil || ok {
		t.Fatalf("profile after rehash: %v %v", ok, err)
	}
	current, _ := p.GetUser(original.Id)
	if current.Password != "new-hash" {
		t.Fatal("profile restored old hash")
	}
	p.DeleteUser(original.Id)
	if ok, err := p.UpdateUserProfile(current, update); err != nil || ok {
		t.Fatalf("deleted profile: %v %v", ok, err)
	}
	if _, ok := p.GetUser(original.Id); ok {
		t.Fatal("profile recreated deleted user")
	}
}
