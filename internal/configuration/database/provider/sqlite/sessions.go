package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

type schemaSessions struct {
	Id         string
	RenewAt    int64
	ValidUntil int64
	UserId     int
	Successor  string
}

// GetSession returns the session with the given ID or false if not a valid ID
func (p DatabaseProvider) GetSession(id string) (models.Session, bool) {
	var rowResult schemaSessions
	row := p.sqliteDb.QueryRow("SELECT Id, RenewAt, ValidUntil, UserId, Successor FROM Sessions WHERE Id = ?", id)
	err := row.Scan(&rowResult.Id, &rowResult.RenewAt, &rowResult.ValidUntil, &rowResult.UserId, &rowResult.Successor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Session{}, false
		}
		helper.Check(err)
		return models.Session{}, false
	}
	result := models.Session{
		RenewAt:    rowResult.RenewAt,
		ValidUntil: rowResult.ValidUntil,
		UserId:     rowResult.UserId,
		Successor:  rowResult.Successor,
	}
	return result, true
}

// SaveSession stores the given session. After the expiry passed, it will be deleted automatically
func (p DatabaseProvider) SaveSession(id string, session models.Session) {
	newData := schemaSessions{
		Id:         id,
		RenewAt:    session.RenewAt,
		ValidUntil: session.ValidUntil,
		UserId:     session.UserId,
		Successor:  session.Successor,
	}

	_, err := p.sqliteDb.Exec("INSERT OR REPLACE INTO Sessions (Id, RenewAt, ValidUntil, UserId, Successor) VALUES (?, ?, ?, ?, ?)",
		newData.Id, newData.RenewAt, newData.ValidUntil, newData.UserId, newData.Successor)
	helper.Check(err)
}

// DeleteSession deletes a session with the given ID
func (p DatabaseProvider) DeleteSession(id string) {
	_, err := p.sqliteDb.Exec("DELETE FROM Sessions WHERE Id = ?", id)
	helper.Check(err)
}

// DeleteAllSessions logs all users out
func (p DatabaseProvider) DeleteAllSessions() {
	//goland:noinspection SqlWithoutWhere
	_, err := p.sqliteDb.Exec("DELETE FROM Sessions")
	helper.Check(err)
}

// DeleteAllSessionsByUser logs the specific users out
func (p DatabaseProvider) DeleteAllSessionsByUser(userId int) {
	_, err := p.sqliteDb.Exec("DELETE FROM Sessions WHERE UserId = ?", userId)
	helper.Check(err)
}

func (p DatabaseProvider) cleanExpiredSessions() {
	_, err := p.sqliteDb.Exec("DELETE FROM Sessions WHERE Sessions.ValidUntil < ?", time.Now().Unix())
	helper.Check(err)
}

// Retry-safe for a process stopped between schema mutation and version update.
func (p DatabaseProvider) initSessionLineage() error {
	var count int
	if err := p.sqliteDb.QueryRow("SELECT count(*) FROM pragma_table_info('Sessions') WHERE name = 'Successor'").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	return p.rawSqlite(`ALTER TABLE Sessions ADD COLUMN Successor TEXT NOT NULL DEFAULT '';`)
}
