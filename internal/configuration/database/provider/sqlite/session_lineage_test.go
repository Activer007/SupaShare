package sqlite

import (
	"github.com/Activer007/SupaShare/internal/models"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLineageSchemaUpgrade(t *testing.T) {
	p, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "upgrade.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = p.sqliteDb.Exec("ALTER TABLE Sessions DROP COLUMN Successor"); err != nil {
		t.Fatal(err)
	}
	p.SetDbVersion(20)
	if _, err = p.sqliteDb.Exec("INSERT INTO Sessions (Id,RenewAt,ValidUntil,UserId) VALUES ('old',1,?,91)", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	p.Upgrade(20)
	p.SetDbVersion(DatabaseSchemeVersion)
	if _, ok := p.GetSession("old"); ok {
		t.Fatal("upgrade retained pre-lineage credential")
	}
	session := models.Session{UserId: 91, ValidUntil: time.Now().Add(time.Hour).Unix(), Successor: "new"}
	p.SaveSession("alias", session)
	got, ok := p.GetSession("alias")
	if !ok || got != session {
		t.Fatal("alias not persisted")
	}
}
