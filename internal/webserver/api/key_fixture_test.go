package api

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

// generateNewKey generates and saves a new API key
func generateNewKey(defaultPermissions bool, userId int, friendlyName, filerequstId string) models.ApiKey {
	if friendlyName == "" {
		friendlyName = "Unnamed key"
	}
	newKey := models.ApiKey{
		Id:              helper.GenerateRandomString(LengthApiKey),
		PublicId:        helper.GenerateRandomString(LengthPublicId),
		FriendlyName:    friendlyName,
		Permissions:     models.ApiPermDefault,
		IsSystemKey:     false,
		UserId:          userId,
		UploadRequestId: filerequstId,
	}
	if !defaultPermissions {
		newKey.Permissions = models.ApiPermNone
	}
	database.SaveApiKey(newKey)
	return newKey
}
