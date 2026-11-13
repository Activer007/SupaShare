package redis

import (
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
	"testing"
	"time"
)

func TestSessionLineageUpgradeAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	p, err := New(models.DbConnection{HostUrl: server.Addr(), RedisPrefix: "lineage_"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	session := models.Session{UserId: 91, ValidUntil: time.Now().Add(time.Hour).Unix()}
	p.SaveSession("old", session)
	p.SetDbVersion(12)
	p.Upgrade(12)
	p.SetDbVersion(DatabaseSchemeVersion)
	if _, ok := p.GetSession("old"); ok {
		t.Fatal("upgrade retained pre-lineage credential")
	}
	session.Successor = "successor"
	p.SaveSession("alias", session)
	got, ok := p.GetSession("alias")
	if !ok || got != session {
		t.Fatal("alias lost in Redis hash")
	}
	server.FastForward(2 * time.Hour)
	if _, ok := p.GetSession("alias"); ok {
		t.Fatal("alias exceeded original cookie lifetime")
	}
}
