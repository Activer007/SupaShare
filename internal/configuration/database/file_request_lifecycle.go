package database

import (
	"context"
	"github.com/Activer007/SupaShare/internal/models"
)

func CreateFileRequestLimited(ctx context.Context, r models.FileRequest, k models.ApiKey) (bool, error) {
	return db.CreateFileRequestLimited(ctx, r, k)
}
func UpdateFileRequest(ctx context.Context, old, r models.FileRequest) (bool, error) {
	return db.UpdateFileRequest(ctx, old, r)
}
func ListFileRequestsPage(ctx context.Context, owner int, before string, limit int) ([]models.FileRequest, error) {
	return db.ListFileRequestsPage(ctx, owner, before, limit)
}
func ReadFileRequestFiles(ctx context.Context, id string) (map[string]models.File, error) {
	return db.ReadFileRequestFiles(ctx, id)
}
func PruneFileRequests(ctx context.Context, now int64, limit int) (int, error) {
	return db.PruneFileRequests(ctx, now, limit)
}
