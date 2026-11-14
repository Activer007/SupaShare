package tokengeneration

import (
	"errors"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test/testconfiguration"
	"testing"
)

func TestN04GenerateTemporaryQuota(t *testing.T) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	defer testconfiguration.Delete()
	user := models.User{Id: 6655, Name: "Synthetic"}
	var first string
	for i := 0; i < models.TemporaryApiKeyMaxPerUser; i++ {
		token, _, err := Generate(user, models.ApiPermEdit)
		if token == "" || err != nil {
			t.Fatal(token, err)
		}
		if i == 0 {
			first = token
		}
	}
	token, expiry, err := Generate(user, models.ApiPermEdit)
	if token != "" || expiry != 0 || !errors.Is(err, models.ErrTemporaryApiKeyQuotaExceeded) {
		t.Fatal("overflow minted credential", token, expiry, err)
	}
	if _, exists := database.GetApiKey(first); !exists {
		t.Fatal("issuance evicted another tab credential")
	}
	user.Id++
	if token, _, err := Generate(user, models.ApiPermView); token == "" || err != nil {
		t.Fatal("one user consumed other's quota", err)
	}
}
