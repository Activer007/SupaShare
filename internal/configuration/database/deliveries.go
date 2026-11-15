package database

import (
	"context"

	"github.com/Activer007/SupaShare/internal/models"
)

func InsertDelivery(ctx context.Context, delivery models.Delivery) (bool, error) {
	return db.InsertDelivery(ctx, delivery)
}

func ReadDelivery(ctx context.Context, token string) (models.Delivery, bool, error) {
	return db.ReadDelivery(ctx, token)
}

func ListDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	return db.ListDeliveries(ctx, ownerID)
}

func RevokeDelivery(ctx context.Context, token string, ownerID int, now int64) (bool, error) {
	return db.RevokeDelivery(ctx, token, ownerID, now)
}

// CreateDeliveryLimited is the online admission path; InsertDelivery is reserved for offline migration.
func CreateDeliveryLimited(ctx context.Context, d models.Delivery, now int64) (bool, error) {
	return db.CreateDeliveryLimited(ctx, d, now)
}
func ListDeliveriesPage(ctx context.Context, ownerID int, before string, limit int) ([]models.Delivery, error) {
	return db.ListDeliveriesPage(ctx, ownerID, before, limit)
}
func PruneDeliveries(ctx context.Context, now int64, limit int) (int, error) {
	return db.PruneDeliveries(ctx, now, limit)
}
