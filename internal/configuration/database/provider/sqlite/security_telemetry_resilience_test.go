package sqlite

import (
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestUpdateUserLastOnlineResilience(t *testing.T) {
	p := newQuotaDatabase(t)
	user := models.User{
		Id:         42,
		Name:       "telemetry-user",
		UserLevel:  models.UserLevelUser,
		LastOnline: 100,
	}
	p.SaveUser(user, false)

	p.UpdateUserLastOnline(user.Id)

	updated, ok := p.GetUser(user.Id)
	if !ok {
		t.Fatal("user not found")
	}
	if updated.LastOnline <= 100 {
		t.Fatalf("expected LastOnline > 100, got %d", updated.LastOnline)
	}

	// Close the underlying database to simulate DB failure / lock / disconnection
	p.Close()

	// Calling UpdateUserLastOnline on closed DB must log and not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("UpdateUserLastOnline panicked on closed DB: %v", r)
		}
	}()
	p.UpdateUserLastOnline(user.Id)
}

func TestGCOnClosedDBDoesNotPanic(t *testing.T) {
	p := newQuotaDatabase(t)
	p.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("cleanApiKeys / cleanExpiredSessions panicked on closed DB: %v", r)
		}
	}()
	p.cleanApiKeys()
	p.cleanExpiredSessions()
	p.UpdateTimeApiKey(models.ApiKey{Id: "closed-key", LastUsed: time.Now().Unix()})
}
