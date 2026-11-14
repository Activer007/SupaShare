package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

type schemaUser struct {
	Id             int
	Name           string
	Password       sql.NullString
	Permissions    models.UserPermission
	UserLevel      models.UserRank
	LastOnline     int64
	ResetPassword  int
	AuthGeneration int64
}

func (s schemaUser) ToUser() models.User {
	pw := ""
	if s.Password.Valid {
		pw = s.Password.String
	}
	return models.User{
		Id:             s.Id,
		Name:           s.Name,
		Permissions:    s.Permissions,
		UserLevel:      s.UserLevel,
		LastOnline:     s.LastOnline,
		Password:       pw,
		ResetPassword:  s.ResetPassword == 1,
		AuthGeneration: s.AuthGeneration,
	}
}

// GetAllUsers returns a map with all users
func (p DatabaseProvider) GetAllUsers() []models.User {
	var result []models.User
	rows, err := p.sqliteDb.Query("SELECT * FROM Users ORDER BY Userlevel , LastOnline DESC, Name ")
	helper.Check(err)
	// Offline migration also reads pre-generation source databases without altering them.
	defer rows.Close()
	columns, err := rows.Columns()
	helper.Check(err)
	for rows.Next() {
		row := schemaUser{}
		if len(columns) == 7 {
			err = rows.Scan(&row.Id, &row.Name, &row.Password, &row.Permissions, &row.UserLevel, &row.LastOnline, &row.ResetPassword)
		} else {
			err = rows.Scan(&row.Id, &row.Name, &row.Password, &row.Permissions, &row.UserLevel, &row.LastOnline, &row.ResetPassword, &row.AuthGeneration)
		}
		helper.Check(err)
		result = append(result, row.ToUser())
	}
	return result
}

func (p DatabaseProvider) getUserWithConstraint(isName bool, searchValue any) (models.User, bool) {
	rowResult := schemaUser{}
	query := "SELECT * FROM Users WHERE Id = ?"
	if isName {
		query = "SELECT * FROM Users WHERE Name = ?"
	}
	row := p.sqliteDb.QueryRow(query, searchValue)
	err := row.Scan(&rowResult.Id, &rowResult.Name, &rowResult.Password, &rowResult.Permissions, &rowResult.UserLevel, &rowResult.LastOnline, &rowResult.ResetPassword, &rowResult.AuthGeneration)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, false
		}
		helper.Check(err)
		return models.User{}, false
	}
	user := rowResult.ToUser()
	return user, true
}

// GetUser returns a models.User if valid or false if the ID is not valid
func (p DatabaseProvider) GetUser(id int) (models.User, bool) {
	return p.getUserWithConstraint(false, id)
}

// GetUserByName returns a models.User if valid or false if the name is not valid
func (p DatabaseProvider) GetUserByName(username string) (models.User, bool) {
	return p.getUserWithConstraint(true, username)
}

// SaveUser is a trusted creation/import upsert, not an authentication mutation API.
// Use narrow compare-and-update methods for live credential and profile changes.
// If isNewUser is true, a new Id will be generated.
func (p DatabaseProvider) SaveUser(user models.User, isNewUser bool) {
	resetpw := 0
	if user.ResetPassword {
		resetpw = 1
	}
	if isNewUser {
		_, err := p.sqliteDb.Exec("INSERT INTO Users (Name, Password, Permissions, Userlevel, LastOnline, ResetPassword, AuthGeneration) VALUES  (?, ?, ?, ?, ?, ?, ?)",
			user.Name, user.Password, user.Permissions, user.UserLevel, user.LastOnline, resetpw, user.AuthGeneration)
		helper.Check(err)
	} else {
		_, err := p.sqliteDb.Exec(`INSERT INTO Users (Id, Name, Password, Permissions, Userlevel, LastOnline, ResetPassword, AuthGeneration)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(Id) DO UPDATE SET Name=excluded.Name, Password=excluded.Password,
Permissions=excluded.Permissions, Userlevel=excluded.Userlevel, LastOnline=excluded.LastOnline,
ResetPassword=excluded.ResetPassword,
AuthGeneration=Users.AuthGeneration + CASE WHEN COALESCE(Users.Password,'') != excluded.Password OR Users.ResetPassword != excluded.ResetPassword THEN 1 ELSE 0 END
`,
			user.Id, user.Name, user.Password, user.Permissions, user.UserLevel, user.LastOnline, resetpw, user.AuthGeneration)
		helper.Check(err)
	}
}

// UpdateUserLastOnline writes the last online time to the database
func (p DatabaseProvider) UpdateUserLastOnline(id int) {
	timeNow := time.Now().Unix()
	_, err := p.sqliteDb.Exec("UPDATE Users SET LastOnline= ? WHERE Id = ?", timeNow, id, timeNow)
	helper.Check(err)
}

// DeleteUser deletes a user with the given ID
func (p DatabaseProvider) DeleteUser(id int) {
	tx, err := p.sqliteDb.Begin()
	helper.Check(err)
	defer tx.Rollback()
	_, err = tx.Exec("DELETE FROM OIDCIdentities WHERE UserID = ?", id)
	helper.Check(err)
	_, err = tx.Exec("DELETE FROM Users WHERE Id = ?", id)
	helper.Check(err)
	helper.Check(tx.Commit())
}

// UpdateUserResetCredentials never replaces a user row or restores stale profile
// fields. Matching authorization and credential state makes stale resets fail.
func (p DatabaseProvider) UpdateUserResetCredentials(expected models.User, password string, reset bool) (bool, error) {
	if expected.Id <= 0 {
		return false, nil
	}
	result, err := p.sqliteDb.Exec(`UPDATE Users SET Password = ?, ResetPassword = ?, AuthGeneration = AuthGeneration + 1
		WHERE Id = ? AND COALESCE(Password, '') = ? AND ResetPassword = ?
		AND Userlevel = ? AND Permissions = ? AND AuthGeneration = ?`,
		password, reset, expected.Id, expected.Password, expected.ResetPassword, expected.UserLevel, expected.Permissions, expected.AuthGeneration)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// RehashPassword upgrades only the exact credential that was verified. It must
// never overwrite a concurrent reset or restore unrelated user fields.
func (p DatabaseProvider) RehashPassword(expected models.User, password string) (bool, error) {
	result, err := p.sqliteDb.Exec(`UPDATE Users SET Password=? WHERE Id=? AND COALESCE(Password,'')=? AND ResetPassword=? AND AuthGeneration=?`, password, expected.Id, expected.Password, expected.ResetPassword, expected.AuthGeneration)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// UpdateUserProfile is a narrow compare-and-update; stale requests cannot restore credentials or deleted users.
func (p DatabaseProvider) UpdateUserProfile(expected, updated models.User) (bool, error) {
	if expected.Id <= 0 || expected.Id != updated.Id {
		return false, nil
	}
	result, err := p.sqliteDb.Exec(`UPDATE Users SET Name=?, Permissions=?, Userlevel=? WHERE Id=? AND COALESCE(Password,'')=? AND ResetPassword=? AND AuthGeneration=? AND Permissions=? AND Userlevel=?`, updated.Name, updated.Permissions, updated.UserLevel, expected.Id, expected.Password, expected.ResetPassword, expected.AuthGeneration, expected.Permissions, expected.UserLevel)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
