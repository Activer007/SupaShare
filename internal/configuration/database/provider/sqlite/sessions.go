package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

type schemaSessions struct {
	Id             string
	RenewAt        int64
	ValidUntil     int64
	UserId         int
	Successor      string
	AuthGeneration int64
}

// GetSession returns the session with the given ID or false if not a valid ID
func (p DatabaseProvider) GetSession(id string) (models.Session, bool) {
	var rowResult schemaSessions
	row := p.sqliteDb.QueryRow("SELECT Id, RenewAt, ValidUntil, UserId, Successor, AuthGeneration FROM Sessions WHERE Id = ?", id)
	err := row.Scan(&rowResult.Id, &rowResult.RenewAt, &rowResult.ValidUntil, &rowResult.UserId, &rowResult.Successor, &rowResult.AuthGeneration)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Session{}, false
		}
		helper.Check(err)
		return models.Session{}, false
	}
	result := models.Session{
		RenewAt:        rowResult.RenewAt,
		ValidUntil:     rowResult.ValidUntil,
		UserId:         rowResult.UserId,
		Successor:      rowResult.Successor,
		AuthGeneration: rowResult.AuthGeneration,
	}
	return result, true
}

// SaveSession stores the given session. After the expiry passed, it will be deleted automatically
func (p DatabaseProvider) SaveSession(id string, session models.Session) {
	newData := schemaSessions{
		Id:             id,
		RenewAt:        session.RenewAt,
		ValidUntil:     session.ValidUntil,
		UserId:         session.UserId,
		Successor:      session.Successor,
		AuthGeneration: session.AuthGeneration,
	}

	_, err := p.sqliteDb.Exec("INSERT OR REPLACE INTO Sessions (Id, RenewAt, ValidUntil, UserId, Successor, AuthGeneration) VALUES (?, ?, ?, ?, ?, ?)",
		newData.Id, newData.RenewAt, newData.ValidUntil, newData.UserId, newData.Successor, newData.AuthGeneration)
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

// SaveSessionIfCredentials linearizes issuance against password/reset changes.
func (p DatabaseProvider) SaveSessionIfCredentials(id string, session models.Session, expected models.User) (bool, error) {
	if expected.Id <= 0 || session.UserId != expected.Id || session.AuthGeneration != expected.AuthGeneration {
		return false, nil
	}
	result, err := p.sqliteDb.Exec(`INSERT INTO Sessions (Id, RenewAt, ValidUntil, UserId, Successor, AuthGeneration)
SELECT ?,?,?,?,?,? FROM Users WHERE Id=? AND COALESCE(Password,'')=? AND ResetPassword=? AND AuthGeneration=?`, id, session.RenewAt, session.ValidUntil, session.UserId, session.Successor, session.AuthGeneration, expected.Id, expected.Password, expected.ResetPassword, expected.AuthGeneration)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (p DatabaseProvider) initAuthGeneration() error {
	for _, table := range []string{"Users", "Sessions"} {
		var count int
		if err := p.sqliteDb.QueryRow("SELECT count(*) FROM pragma_table_info('" + table + "') WHERE name='AuthGeneration'").Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err := p.rawSqlite("ALTER TABLE " + table + " ADD COLUMN AuthGeneration INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
	}
	return nil
}
