//go:build test

package sessionmanager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

// All data is synthetic, using temporary SQLite or an in-process Redis server.
func TestOAuthAbsoluteRecheckDeadline(t *testing.T) {
	// Isolate global backend switching from asynchronous updates in other tests.
	const child = "SUPASHARE_OAUTH_DEADLINE_CHILD"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOAuthAbsoluteRecheckDeadline$", "-test.v")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), child+"=1")
		output, err := cmd.CombinedOutput()
		t.Logf("isolated OAuth deadline contract:\n%s", output)
		if err != nil {
			t.Fatalf("OAuth deadline contract failed: %v", err)
		}
		return
	}

	database.Close()
	defer configuration.ConnectDatabase()
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "sessions.sqlite")}
			var redis *miniredis.Miniredis
			if backend == "redis" {
				redis = miniredis.RunT(t)
				cfg = models.DbConnection{Type: 1, HostUrl: redis.Addr(), RedisPrefix: "deadline_"}
			}
			database.Connect(cfg)
			defer database.Close()
			user := models.User{Id: 9088, Name: "synthetic-deadline-user", UserLevel: models.UserLevelUser}
			database.SaveUser(user, false)
			for _, hours := range []int{1, 6, 12, 24, 72, 168, 336, 720} {
				t.Run(fmt.Sprint(hours), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						if redis != nil {
							redis.SetTime(time.Now())
						}
						w := httptest.NewRecorder()
						CreateSession(w, true, hours, user.Id)
						cookie := w.Result().Cookies()[0]
						original, ok := database.GetSession(cookie.Value)
						if !ok {
							t.Fatal("missing initial session")
						}
						deadline := original.ValidUntil
						for time.Now().Add(12*time.Hour+time.Second).Unix() < deadline {
							time.Sleep(12*time.Hour + time.Second)
							if redis != nil {
								redis.SetTime(time.Now())
							}
							old := cookie.Value
							w = httptest.NewRecorder()
							r := httptest.NewRequest(http.MethodGet, "/admin", nil)
							r.AddCookie(cookie)
							if _, ok := IsValidSession(w, r, true, hours); !ok {
								t.Fatal("live session rejected")
							}
							synctest.Wait() // last-online update must complete before backend cleanup
							cookies := w.Result().Cookies()
							if len(cookies) != 1 {
								t.Fatal("rotation did not set one cookie")
							}
							cookie = cookies[0]
							next, ok := database.GetSession(cookie.Value)
							if !ok || next.ValidUntil != deadline || cookie.Expires.Unix() != deadline {
								t.Fatalf("rotation extended recheck: session=%+v cookie=%v original=%d", next, cookie.Expires, deadline)
							}
							if cookie.Value == old || next.RenewAt != time.Now().Add(12*time.Hour).Unix() {
								t.Fatal("ID/renewal not rotated")
							}
							if _, ok := database.GetSession(old); ok {
								t.Fatal("predecessor still exists")
							}
						}
						time.Sleep(time.Until(time.Unix(deadline+1, 0)))
						if redis != nil {
							redis.SetTime(time.Now())
						}
						w = httptest.NewRecorder()
						r := httptest.NewRequest(http.MethodGet, "/admin", nil)
						r.AddCookie(cookie)
						if _, ok := IsValidSession(w, r, true, hours); ok {
							t.Fatal("expired OAuth session accepted")
						}
						if len(w.Result().Cookies()) != 0 {
							t.Fatal("expired session rotated")
						}
						if _, ok := database.GetSession(cookie.Value); ok {
							t.Fatal("expired session not removed")
						}
						// A new authenticated login, unlike rotation, receives a new deadline.
						w = httptest.NewRecorder()
						CreateSession(w, true, hours, user.Id)
						fresh, ok := database.GetSession(w.Result().Cookies()[0].Value)
						if !ok || fresh.ValidUntil != time.Now().Add(time.Duration(hours)*time.Hour).Unix() || fresh.ValidUntil <= deadline {
							t.Fatal("new login did not establish a new deadline")
						}
					})
				})
			}
			t.Run("password-sliding-expiry", func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					if redis != nil {
						redis.SetTime(time.Now())
					}
					id, original := buildSession(false, 1, user.Id)
					database.SaveSession(id, original)
					time.Sleep(12*time.Hour + time.Second)
					if redis != nil {
						redis.SetTime(time.Now())
					}
					w := httptest.NewRecorder()
					if !useSession(w, id, original, false, 1) {
						t.Fatal("password session rejected")
					}
					synctest.Wait()
					next, ok := database.GetSession(w.Result().Cookies()[0].Value)
					if !ok || next.ValidUntil != time.Now().Add(cookieLifeAdmin).Unix() || next.ValidUntil <= original.ValidUntil {
						t.Fatal("password sliding expiration changed")
					}
				})
			})
		})
	}
}
