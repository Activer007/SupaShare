package delivery

import (
	"context"
	"errors"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
	"time"
)

func TestN12DeliveryLifetime(t *testing.T) {
	deliveryScene(t)
	ctx := context.Background()
	input := Input{Title: "bounded", FileIDs: []string{"first", "second"}}
	before := time.Now().Unix()
	d, err := Create(ctx, 1, input)
	if err != nil || d.ExpiresAt < before+models.DeliveryDefaultLifetime || d.ExpiresAt > time.Now().Unix()+models.DeliveryDefaultLifetime {
		t.Fatalf("default lifetime %+v %v", d, err)
	}
	input.ExpiresAt = time.Now().Unix() + models.DeliveryMaxLifetime + 60
	if _, err = Create(ctx, 1, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("excess lifetime: %v", err)
	}
	owner, _ := database.GetUser(1)
	owner.UserLevel = models.UserLevelAdmin
	database.SaveUser(owner, false)
	input.ExpiresAt = 0
	d, err = Create(ctx, 1, input)
	if err != nil || d.ExpiresAt != 0 {
		t.Fatalf("admin permanent %+v %v", d, err)
	}
	// Pruning the delivery never changes an underlying public share.
	_, err = database.RevokeDelivery(ctx, d.Token, 1, before)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := database.PruneDeliveries(ctx, before+models.DeliveryRetentionSeconds, 100); err != nil || n != 1 {
		t.Fatalf("prune %d %v", n, err)
	}
	if _, ok, err := database.ReadRecoveryFile(ctx, "first"); err != nil || !ok {
		t.Fatal("source removed", err)
	}
}
