//go:build test

package sessionmanager

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

// Two requests rendezvous after password verification, before session commit.
// Revocation deliberately does NOT sweep sessions: the generation itself must
// protect both a late issuance and an already issued (including renewed) token.
func TestN02PasswordResetIssuanceRace(t *testing.T) {
	const child = "SUPASHARE_N02_SESSION_CHILD"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestN02PasswordResetIssuanceRace$", "-test.v")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), child+"=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	database.Close()
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "auth.sqlite")}
			if backend == "redis" {
				server := miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: server.Addr(), RedisPrefix: "n02_"}
			}
			database.Connect(cfg)
			defer database.Close()
			original := models.User{Id: 8202, Name: "n02", Password: "verified-old-hash", ResetPassword: true, UserLevel: models.UserLevelUser}
			database.SaveUser(original, false)
			original, _ = database.GetUser(original.Id)
			early := httptest.NewRecorder()
			if !CreateSessionForUser(early, false, 0, original) {
				t.Fatal("initial issuance failed")
			}
			cookie := early.Result().Cookies()[0]
			verified := make(chan struct{})
			resume := make(chan struct{})
			result := make(chan bool)
			late := httptest.NewRecorder()
			go func() { close(verified); <-resume; result <- CreateSessionForUser(late, false, 0, original) }()
			<-verified
			changed, err := database.UpdateUserResetCredentials(original, "reset-hash", true)
			if err != nil || !changed {
				t.Fatalf("reset: %v %v", changed, err)
			}
			close(resume)
			if <-result || len(late.Result().Cookies()) != 0 {
				t.Fatal("stale verified request received session cookie")
			}
			req := httptest.NewRequest("GET", "/admin", nil)
			req.AddCookie(cookie)
			if _, ok := IsValidSession(httptest.NewRecorder(), req, false, 0); ok {
				t.Fatal("old session accepted after reset")
			}
			oldSession, _ := database.GetSession(cookie.Value)
			replacement := oldSession
			replacement.RenewAt = time.Now().Add(time.Hour).Unix()
			if database.RotateSession(cookie.Value, "stale-renewal", oldSession, replacement) {
				t.Fatal("stale session renewed")
			}
			if changed, err := database.UpdateUserResetCredentials(original, "attacker-selected-hash", false); err != nil || changed {
				t.Fatal("in-flight reset-required session overwrote completed reset", err)
			}
			fresh, _ := database.GetUser(original.Id)
			current := httptest.NewRecorder()
			if !CreateSessionForUser(current, false, 0, fresh) {
				t.Fatal("fresh credentials denied")
			}
			stored, ok := database.GetSession(current.Result().Cookies()[0].Value)
			if !ok || stored.AuthGeneration != fresh.AuthGeneration {
				t.Fatal("fresh session generation mismatch")
			}

		})
	}
}
