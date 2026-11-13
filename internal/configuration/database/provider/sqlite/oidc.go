package sqlite

import (
	"database/sql"
	"errors"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"strings"
)

func (p DatabaseProvider) initOIDCIdentities() error {
	// No cascading FK: SaveUser historically uses INSERT OR REPLACE.
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS OIDCIdentities (
 Issuer TEXT NOT NULL, Subject TEXT NOT NULL, UserID INTEGER NOT NULL UNIQUE,
 PRIMARY KEY (Issuer, Subject)) WITHOUT ROWID;`)
}

func (p DatabaseProvider) BindOIDCIdentity(issuer, subject string, userID int) error {
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: userID}).Valid() {
		return models.ErrOIDCInvalidIdentity
	}
	tx, err := p.sqliteDb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Atomic INSERT SELECT refuses absent users and never replaces either binding.
	_, err = tx.Exec(`INSERT INTO OIDCIdentities (Issuer,Subject,UserID)
 SELECT ?,?,Id FROM Users WHERE Id=? ON CONFLICT DO NOTHING`, issuer, subject, userID)
	if err != nil {
		return err
	}
	var bound int
	err = tx.QueryRow(`SELECT UserID FROM OIDCIdentities WHERE Issuer=? AND Subject=? AND EXISTS(SELECT 1 FROM Users WHERE Id=?)`, issuer, subject, userID).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrOIDCBindingConflict
	}
	if err != nil {
		return err
	}
	if bound != userID {
		return models.ErrOIDCBindingConflict
	}
	return tx.Commit()
}

func (p DatabaseProvider) GetOIDCUser(issuer, subject string) (models.User, bool, error) {
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: 1}).Valid() {
		return models.User{}, false, models.ErrOIDCInvalidIdentity
	}
	var u schemaUser
	err := p.sqliteDb.QueryRow(`SELECT u.* FROM Users u JOIN OIDCIdentities i ON u.Id=i.UserID WHERE i.Issuer=? AND i.Subject=?`, issuer, subject).Scan(&u.Id, &u.Name, &u.Password, &u.Permissions, &u.UserLevel, &u.LastOnline, &u.ResetPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, false, nil
	}
	if err != nil {
		return models.User{}, false, err
	}
	return u.ToUser(), true, nil
}

func (p DatabaseProvider) ListOIDCIdentities() ([]models.OIDCIdentity, error) {
	var present bool
	if err := p.sqliteDb.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='OIDCIdentities')").Scan(&present); err != nil {
		return nil, err
	}
	if !present {
		return nil, uploadquota.ErrNotFound
	}
	rows, err := p.sqliteDb.Query(`SELECT Issuer,Subject,UserID FROM OIDCIdentities`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []models.OIDCIdentity
	for rows.Next() {
		var i models.OIDCIdentity
		if err = rows.Scan(&i.Issuer, &i.Subject, &i.UserID); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

// CreateOIDCUser never adopts an existing name, even if supplied by a verified provider.
func (p DatabaseProvider) CreateOIDCUser(issuer, subject, name string, permissions models.UserPermission) (models.User, error) {
	name = strings.ToLower(name)
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: 1}).Valid() || name == "" {
		return models.User{}, models.ErrOIDCInvalidIdentity
	}
	tx, err := p.sqliteDb.Begin()
	if err != nil {
		return models.User{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO Users (Name,Password,Permissions,Userlevel,LastOnline,ResetPassword) VALUES (?,'',?,2,0,0)`, name, permissions)
	if err != nil {
		return models.User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.User{}, err
	}
	_, err = tx.Exec(`INSERT INTO OIDCIdentities (Issuer,Subject,UserID) VALUES (?,?,?)`, issuer, subject, id)
	if err != nil {
		return models.User{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.User{}, err
	}
	return models.User{Id: int(id), Name: name, Permissions: permissions, UserLevel: models.UserLevelUser}, nil
}
