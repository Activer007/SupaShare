package api

import (
	"errors"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
	"net/http"
)

func validateApiKeyName(name string) error {
	if len(name) > models.MaxApiKeyNameBytes {
		return errors.New("API key name exceeds 256 UTF-8 bytes")
	}
	return validateDisplayName(name)
}

// Creation uses an atomic provider admission, not a read/count followed by Save.
// Existing credentials and the system tokens needed to delete them stay usable.
func createUserApiKey(defaultPermissions bool, userID int, name string, insert func(models.ApiKey) (bool, error)) (models.ApiKey, error) {
	if err := validateApiKeyName(name); err != nil {
		return models.ApiKey{}, err
	}
	if name == "" {
		name = "Unnamed key"
	}
	for attempt := 0; attempt < 5; attempt++ {
		key := models.ApiKey{Id: helper.GenerateRandomString(LengthApiKey), PublicId: helper.GenerateRandomString(LengthPublicId), FriendlyName: name, UserId: userID}
		if defaultPermissions {
			key.Permissions = models.ApiPermDefault
		}
		inserted, err := insert(key)
		if err != nil {
			return models.ApiKey{}, err
		}
		if inserted {
			return key, nil
		}
	}
	return models.ApiKey{}, errors.New("unable to allocate API credential")
}

// Never upsert an edited credential: concurrent deletion must not recreate it
// outside the quota gate (or undo the user's revocation).
func updateApiKeyProperties(w http.ResponseWriter, key models.ApiKey) {
	updated, err := database.UpdateApiKeyProperties(key)
	if err != nil {
		sendError(w, http.StatusInternalServerError, errorcodes.InternalServer, "Could not update API key")
		return
	}
	if !updated {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "API key was deleted")
	}
}
