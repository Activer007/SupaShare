# N11: bounded E2E metadata leases

## Scope

CSV finding #11 (`acf_c1da6ba53a7b585e7adcbb5de0590343`): replace the cancellation-insensitive per-user mutex waiter queue with a holder-token lease. This change does not change encryption formats or replay metadata writes.

- Acquisition is immediate: an occupied user returns 409; 1,024 occupied users exhaust the process-wide capacity and return 429. There is no waiter goroutine per request. A brief map mutex still serializes bookkeeping.
- A cryptographically random token is returned at acquisition and required in the `e2eLease` header on get, set and unlock. It is scoped to the authenticated user, not a rotating temporary API key. Missing/invalid tokens never release another holder.
- Leases expire after 30 seconds. Cleanup is lazy and bounded by the capacity, without timer callbacks or stale-timer ABA unlocking. Invalid lookups allocate nothing.
- Each admitted get/set pins the holder until its callback completes, including when the nominal lease expires or the holder releases concurrently. Other operations are rejected, not queued. Cancellation is checked before admission; an already-running database callback cannot be forcibly interrupted and may outlast the lease.
- Get/set revalidate current credentials and upload permission. Token possession does not replace authentication.

## Clients and compatibility

Deploy the server, bundled browser assets and CLI together. Old clients without a token fail closed; there is deliberately no tokenless compatibility unlock. Refresh old browser tabs and upgrade separately distributed CLI binaries. The exported CLI `GetE2eInfo(bool)` call shape remains, but it no longer retains a lease after returning.

Browser and CLI acquisition retry only 409/429, at most four attempts within two seconds, with 100/200/300 ms waits. Both request and wait accept cancellation. Read/modify/write callbacks and set requests are never automatically replayed. Every successful acquisition has its own local token and cleanup; failed acquisition does not unlock. Browser cleanup does not reuse an already-aborted signal.

After exhausted contention the client reports a busy/error state, not upload success. Parallel transactions can succeed after short contention, but sustained contention still fails visibly. A file's encrypted bytes may already have uploaded before its metadata transaction fails. Do not assume blindly uploading it again repairs metadata without duplicates. Inspect the failed upload and retained local key/source first; no automatic orphan reconciliation or post-write-uncertainty replay is introduced here. In particular, existing WASM in-memory upload state consumption is not redesigned by this patch.

A lost acquisition response can leave an undisclosed token occupied until expiry. This is bounded by TTL/capacity. The lease registry is process-local, matching the existing architecture: this is not distributed locking for multiple server processes. Multi-instance deployments require separate shared-store coordination before claiming cross-instance serialization.

## Verification

Targeted API tests exercise actual routes, holder/cross-user isolation, stale unlock, current credential revocation, canceled acquisition and 128 contending requests. Lease tests cover single winner, capacity, expiry/ABA, pinned callbacks and panic/error cleanup. CLI/browser tests cover cleanup, four-attempt exhaustion, cancellation, no callback replay and same-user concurrent transactions retaining all changes.

Commands (cloud toolchain uses `GOFLAGS=-buildvcs=false` solely because this worktree cannot supply VCS stamping):

```sh
go test -race -tags test,noaws ./internal/webserver/api/mutex/e2emutex ./internal/webserver/api -run 'TestN11|TestAcquire|TestCancellationCapacity|TestExpiryCleanup|TestWithLease' -count=1
go test -race -tags test,noaws ./cmd/cli-uploader/cliapi -count=1
go test -tags test,noaws ./internal/webserver/api ./internal/webserver/api/mutex/e2emutex ./cmd/cli-uploader/...
node --test build/tests/e2e-lease.test.cjs build/tests/ui-e2e-workspace.test.cjs
go generate ./...
git diff --check
```

These are automated targeted/regression checks, not a real browser/WebAssembly end-to-end upload certification, full-repository regression, or multi-process/real-Redis deployment test.
