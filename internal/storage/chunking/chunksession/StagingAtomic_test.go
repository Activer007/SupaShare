//go:build test && security_chunk

package chunksession

import (
	"context"
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/configuration/database/provider/redis"
	"github.com/Activer007/SupaShare/internal/configuration/database/provider/sqlite"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// Run the shared transition through real SQLite and miniredis atomic providers,
// not through Apply alone. Competing first binds must not oversubscribe a share.
func TestN14AtomicStagingFirstBind(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, tc := range []struct {
			name                string
			users               [2]int
			requests            [2]string
			staging, requestCap int64
			accepted            int
		}{
			{"request", [2]int{77, 77}, [2]string{"a", "a"}, 150, 250, 1},
			{"owner_across_requests", [2]int{77, 77}, [2]string{"a", "b"}, 300, 500, 1},
			{"different_owners", [2]int{77, 78}, [2]string{"a", "b"}, 250, 250, 2},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				s := testStore(t, backend)
				limits := testLimits()
				limits.StagingBytes = 1000
				limits.UserStagingBytes = 500
				limits.RequestStagingBytes = tc.requestCap
				now := time.Now().Unix()
				changes := make([]uploadquota.Change, 2)
				for i := range changes {
					r := uploadquota.Reservation{ID: fmt.Sprintf("fair-%d", i), UserID: tc.users[i], RequestID: tc.requests[i], ExpiresAt: now + 300}
					if _, e := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Reserve, Upload: r, Limits: limits, Now: now}); e != nil {
						t.Fatal(e)
					}
					r.BytesKnown = true
					r.Bytes = 1
					r.StagingBytes = tc.staging
					changes[i] = uploadquota.Change{Action: uploadquota.Bind, Upload: r, Limits: limits, Now: now + 1}
				}
				gate := make(chan struct{})
				results := make(chan error, 2)
				var wg sync.WaitGroup
				for _, c := range changes {
					wg.Add(1)
					go func(c uploadquota.Change) {
						defer wg.Done()
						<-gate
						_, e := s.apply(context.Background(), c)
						results <- e
					}(c)
				}
				close(gate)
				wg.Wait()
				close(results)
				accepted := 0
				for e := range results {
					if e == nil {
						accepted++
					} else if !errors.Is(e, uploadquota.ErrStaging) {
						t.Fatalf("unexpected admission error: %v", e)
					}
				}
				if accepted != tc.accepted {
					t.Fatalf("accepted %d want %d", accepted, tc.accepted)
				}
				ledger, e := s.read(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				bound := 0
				for _, r := range ledger.Records {
					if r.BytesKnown {
						bound++
					} else if r.StagingBytes != 0 {
						t.Fatal("rejected bind mutated reservation", r)
					}
				}
				if bound != accepted {
					t.Fatalf("durable binds %d != admitted %d", bound, accepted)
				}
			})
		}
	}
}

// Exemption is derived from current persisted role, not a caller-controlled flag.
func TestN14ProviderStagingRolePolicy(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := New(t.TempDir())
			var save func(models.User, bool)
			if backend == "redis" {
				p, e := redis.New(models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "fair_role_"})
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveUser
			} else {
				p, e := sqlite.New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "fair.sqlite")})
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveUser
			}
			user := models.User{Id: 77, Name: "fair-role", UserLevel: models.UserLevelUser}
			save(user, false)
			limits := testLimits()
			limits.StagingBytes = 1000
			limits.UserStagingBytes = 500
			limits.RequestStagingBytes = 250
			limits.AdminStagingExempt = true
			now := time.Now().Unix()
			reserve := func(id string, n int64) error {
				_, e := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Reserve, Upload: uploadquota.Reservation{ID: id, UserID: 77, RequestID: "public-owner", BytesKnown: true, Bytes: 1, StagingBytes: n, ExpiresAt: now + 300}, Limits: limits, Now: now})
				return e
			}
			if e := reserve("forged", 600); !errors.Is(e, uploadquota.ErrStaging) {
				t.Fatal("forged exemption accepted", e)
			}
			user.UserLevel = models.UserLevelAdmin
			save(user, false)
			if e := reserve("admin", 600); e != nil {
				t.Fatal("current admin not exempt", e)
			}
			if e := reserve("global", 401); !errors.Is(e, uploadquota.ErrStaging) {
				t.Fatal("admin bypassed global", e)
			}
			user.UserLevel = models.UserLevelUser
			save(user, false)
			if e := reserve("demoted", 1); !errors.Is(e, uploadquota.ErrStaging) {
				t.Fatal("demotion not applied", e)
			}
			ledger, e := s.read(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			r := ledger.Records["admin"]
			// Demotion does not erase already held bytes. Same declaration may finish.
			if _, e = s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Bind, Upload: r, Limits: limits, Now: now + 1}); e != nil {
				t.Fatal("existing bound declaration stranded", e)
			}
		})
	}
}

func TestN14AdminTotalBytesKeepSingleFilePolicy(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := New(t.TempDir())
			var save func(models.User, bool)
			var request func(models.FileRequest)
			var meta func(models.File)
			if backend == "redis" {
				p, e := redis.New(models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "fair_total_"})
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save, request, meta = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveUser, p.SaveFileRequest, p.SaveMetaData
			} else {
				p, e := sqlite.New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "fair.sqlite")})
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(p.Close)
				s.apply, s.read, save, request, meta = p.ApplyUploadQuota, p.ReadUploadQuota, p.SaveUser, p.SaveFileRequest, p.SaveMetaData
			}
			u := models.User{Id: 77, Name: "total-owner", UserLevel: models.UserLevelUser}
			save(u, false)
			request(models.FileRequest{Id: "total-request", UserId: 77, ApiKey: "synthetic-total", MaxFiles: 16, MaxSize: 1})
			limits := testLimits()
			limits.User.Bytes = 1
			limits.Request.Bytes = 1
			limits.Instance.Bytes = 1
			now := time.Now().Unix()
			reserve := func(id string, size int64) error {
				_, e := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Reserve, Upload: uploadquota.Reservation{ID: id, UserID: 77, RequestID: "total-request", BytesKnown: true, Bytes: size, StagingBytes: 4*size + 65536, ExpiresAt: now + 300}, Limits: limits, Now: now, CheckRequestPolicy: true})
				return e
			}
			if e := reserve("ordinary-total", 2); !errors.Is(e, uploadquota.ErrQuota) {
				t.Fatal("ordinary total bypassed", e)
			}
			u.UserLevel = models.UserLevelAdmin
			save(u, false)
			if e := reserve("admin-total", 2); e != nil {
				t.Fatal("admin total limit retained", e)
			}
			if e := reserve("admin-big-file", (1<<20)+1); !errors.Is(e, uploadquota.ErrQuota) {
				t.Fatal("admin single-file policy bypassed", e)
			}
			limits.User.Files = 0
			if e := reserve("admin-file-count", 1); !errors.Is(e, uploadquota.ErrQuota) {
				t.Fatal("admin bypassed file count", e)
			}
			limits.User.Files = 16
			limits.UserActive = 0
			if e := reserve("admin-active-count", 1); !errors.Is(e, uploadquota.ErrActive) {
				t.Fatal("admin bypassed concurrency", e)
			}
			limits.UserActive = 16
			source := models.File{Id: "transfer-source", UserId: 88, SizeBytes: 2, Name: "synthetic.bin", UnlimitedTime: true, UnlimitedDownloads: true}
			meta(source)
			transfer := uploadquota.Change{Action: uploadquota.TransferOwner, Source: &source, Upload: uploadquota.Reservation{ID: source.Id, UserID: 77}, Limits: limits, Now: now}
			u.UserLevel = models.UserLevelUser
			save(u, false)
			if _, e := s.apply(context.Background(), transfer); !errors.Is(e, uploadquota.ErrQuota) {
				t.Fatal("ordinary transfer bypassed total", e)
			}
			u.UserLevel = models.UserLevelAdmin
			save(u, false)
			if _, e := s.apply(context.Background(), transfer); e != nil {
				t.Fatal("admin metadata transfer retained total", e)
			}

		})
	}
}
