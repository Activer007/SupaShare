//go:build test

package database

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

type failedSessionDelete struct {
	dbabstraction.Database
}

func (s failedSessionDelete) DeleteSession(id string) {
	if id == "fault-new" {
		panic("synthetic storage failure")
	}
	s.Database.DeleteSession(id)
}

func TestSessionLineageLogout(t *testing.T) {
	previous := db
	t.Cleanup(func() { db = previous })
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "sessions.sqlite")}
			if backend == "redis" {
				server := miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: server.Addr(), RedisPrefix: "lineage_"}
			}
			store, err := dbabstraction.GetNew(cfg)
			if err != nil {
				t.Fatal(err)
			}
			db = store
			session := models.Session{UserId: 91, RenewAt: time.Now().Add(-time.Minute).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()}
			SaveSession("victim", session)
			shorter := session
			shorter.ValidUntil--
			if RotateSession("victim", "too-short", session, shorter) {
				t.Fatal("rotation shortened lineage retention")
			}
			if _, ok := GetSession("victim"); !ok {
				t.Fatal("rejected shortening revoked original")
			}
			SaveSession("independent-login", session)
			other := session
			other.UserId = 92
			SaveSession("other-user", other)
			if !RotateSession("victim", "successor", session, session) {
				t.Fatal("first rotation failed")
			}
			if _, ok := GetSession("victim"); ok {
				t.Fatal("predecessor authenticates")
			}
			if RotateSession("victim", "second-winner", session, session) {
				t.Fatal("predecessor rotated twice")
			}
			if !RotateSession("successor", "grandchild", session, session) {
				t.Fatal("second generation failed")
			}
			// Reopen the same backing store; no process-local alias registry is involved.
			store.Close()
			store, err = dbabstraction.GetNew(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			db = store
			DeleteSession("victim")
			for _, id := range []string{"victim", "successor", "grandchild"} {
				if _, ok := store.GetSession(id); ok {
					t.Fatalf("logout left %s", id)
				}
			}
			if RotateSession("grandchild", "resurrection", session, session) {
				t.Fatal("revoked snapshot resurrected")
			}
			for _, id := range []string{"independent-login", "other-user"} {
				if _, ok := GetSession(id); !ok {
					t.Fatalf("logout revoked unrelated %s", id)
				}
			}
			SaveSession("fault-old", session)
			if !RotateSession("fault-old", "fault-new", session, session) {
				t.Fatal("fault setup rotation failed")
			}
			db = failedSessionDelete{store}
			func() {
				defer func() {
					if recover() == nil {
						t.Error("expected injected storage failure")
					}
				}()
				DeleteSession("fault-old")
			}()
			db = store
			if alias, ok := store.GetSession("fault-old"); !ok || alias.Successor != "fault-new" {
				t.Fatal("failed delete orphaned successor")
			}
			DeleteSession("fault-old")
			if _, ok := GetSession("fault-new"); ok {
				t.Fatal("logout retry failed")
			}
			// Existing account/global revocation also deletes retained aliases.
			for i, mode := range []string{"user", "all"} {
				old, newID := fmt.Sprintf("old-%d", i), fmt.Sprintf("new-%d", i)
				SaveSession(old, session)
				if !RotateSession(old, newID, session, session) {
					t.Fatal("rotation failed")
				}
				if mode == "user" {
					DeleteAllSessionsByUser(91)
				} else {
					DeleteAllSessions()
				}
				for _, id := range []string{old, newID} {
					if _, ok := store.GetSession(id); ok {
						t.Fatal("bulk revocation retained alias/credential")
					}
				}
			}
		})
	}
}

func TestSessionLogoutRotationOrdering(t *testing.T) {
	previous := db
	t.Cleanup(func() { db = previous })
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "sessions.sqlite")}
			if backend == "redis" {
				server := miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: server.Addr(), RedisPrefix: "race_"}
			}
			store, err := dbabstraction.GetNew(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			db = store
			for i := 0; i < 30; i++ {
				session := models.Session{UserId: 91, RenewAt: time.Now().Add(-time.Minute).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()}
				SaveSession("old", session)
				start, done := make(chan struct{}), make(chan struct{}, 2)
				go func() { <-start; RotateSession("old", "new", session, session); done <- struct{}{} }()
				go func() { <-start; DeleteSession("old"); done <- struct{}{} }()
				close(start)
				<-done
				<-done
				for _, id := range []string{"old", "new"} {
					if _, ok := store.GetSession(id); ok {
						t.Fatal("logout/rotation race left credential")
					}
				}
			}
		})
	}
}
