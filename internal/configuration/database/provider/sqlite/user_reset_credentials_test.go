package sqlite

import (
	"github.com/Activer007/SupaShare/internal/models"
	"path/filepath"
	"testing"
)

func TestUpdateUserResetCredentials(t *testing.T) {
	p, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "reset.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	original := models.User{Id: 71, Name: "target", UserLevel: models.UserLevelUser, Password: "old-hash", Permissions: models.UserPermManageUsers, LastOnline: 17}
	p.SaveUser(original, false)
	// Unrelated concurrent profile updates must survive a valid credential reset.
	current := original
	current.Name = "renamed"
	current.LastOnline = 99
	p.SaveUser(current, false)
	updated, err := p.UpdateUserResetCredentials(original, "new-hash", true)
	if err != nil || !updated {
		t.Fatalf("update = %v, %v", updated, err)
	}
	want := current
	want.Password = "new-hash"
	want.ResetPassword = true
	want.AuthGeneration++
	got, found := p.GetUser(original.Id)
	if !found || got != want {
		t.Fatalf("unrelated fields lost: got %#v, want %#v", got, want)
	}
	// Even the previous successful snapshot cannot be replayed.
	updated, err = p.UpdateUserResetCredentials(original, "replayed", true)
	if err != nil || updated {
		t.Fatalf("stale replay = %v, %v", updated, err)
	}

	for _, tc := range []struct {
		name   string
		change func(*models.User)
	}{
		{"password", func(u *models.User) { u.Password = "changed" }},
		{"reset", func(u *models.User) { u.ResetPassword = true }},
		{"rank", func(u *models.User) { u.UserLevel = models.UserLevelAdmin }},
		{"permissions", func(u *models.User) { u.Permissions = models.UserPermissionNone }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.DeleteUser(original.Id)
			p.SaveUser(original, false)
			changed := original
			tc.change(&changed)
			p.SaveUser(changed, false)
			changed, _ = p.GetUser(original.Id)
			updated, err := p.UpdateUserResetCredentials(original, "must-not-write", true)
			if err != nil || updated {
				t.Fatalf("stale CAS = %v, %v", updated, err)
			}
			got, ok := p.GetUser(original.Id)
			if !ok || got != changed {
				t.Fatal("rejected CAS mutated target")
			}
		})
	}

	// Concurrent reset requests using the same snapshot must not both succeed.
	p.DeleteUser(original.Id)
	p.SaveUser(original, false)
	type outcome struct {
		updated bool
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, password := range []string{"concurrent-a", "concurrent-b"} {
		go func(password string) {
			<-start
			updated, err := p.UpdateUserResetCredentials(original, password, true)
			results <- outcome{updated, err}
		}(password)
	}
	close(start)
	succeeded := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.updated {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent CAS successes = %d, want 1", succeeded)
	}
	p.DeleteUser(original.Id)
	updated, err = p.UpdateUserResetCredentials(original, "must-not-recreate", true)
	if err != nil || updated {
		t.Fatalf("deleted target CAS = %v, %v", updated, err)
	}
	if _, ok := p.GetUser(original.Id); ok {
		t.Fatal("deleted target recreated")
	}
	// Empty password (OIDC-only account) and an unchanged password are supported.
	original.Password = ""
	p.SaveUser(original, false)
	updated, err = p.UpdateUserResetCredentials(original, "", true)
	if err != nil || !updated {
		t.Fatalf("empty password CAS = %v, %v", updated, err)
	}
	got, _ = p.GetUser(original.Id)
	if got.Password != "" || !got.ResetPassword {
		t.Fatal("force-reset state not saved")
	}
}
