//go:build test && security_chunk

package chunksession

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database/provider/redis"
	"github.com/Activer007/SupaShare/internal/configuration/database/provider/sqlite"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/alicebob/miniredis/v2"
)

// A reception ticket may extend the upload lease, never the File Request's
// authorization lifetime. Exercise actual atomic providers after N13 integration.
func TestReceiveTicketDoesNotExtendRequestPolicy(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := New(t.TempDir())
			var save func(models.FileRequest)
			if backend == "redis" {
				p, err := redis.New(models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "progress_policy_"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveFileRequest
			} else {
				p, err := sqlite.New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "policy.sqlite")})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveFileRequest
			}
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			scope := testScopes()[1]
			request := models.FileRequest{Id: scope.RequestID, UserId: scope.UserID, ApiKey: "synthetic-policy", MaxFiles: 16, MaxSize: 32}
			save(request)
			receipt, err := s.Reserve(context.Background(), scope, testLimits())
			if err != nil {
				t.Fatal(err)
			}
			ctx, release, err := s.BeginReceive(context.Background(), scope.UserID, scope.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			request.Expiry = now - 1
			save(request)
			_, err = s.Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 3}, bytes.NewReader([]byte("abc")), 3, 32<<20, testLimits())
			if !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatalf("expired request accepted: %v", err)
			}
			after := testReceipt(t, s, scope)
			if after.BytesKnown || after.ExpiresAt != receipt.ExpiresAt {
				t.Fatal("rejected write bound or renewed reservation", after)
			}
			release()
			now = receipt.ExpiresAt + 1
			if err := s.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			after = testReceipt(t, s, scope)
			if after.State != uploadquota.Expired || !after.StagingReleased {
				t.Fatal("expired request prevented session cleanup", after)
			}
		})
	}
}
