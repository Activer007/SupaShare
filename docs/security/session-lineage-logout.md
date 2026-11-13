# Security group C: rotation lineage and explicit logout

Baseline: `61587fb239111679e6ca85ce83fb01250d4722c9` (merged PR #96). This isolated group C patch was rebased after group B merged; it does not duplicate those changes.

## Scope

- Finding #11: `acf_61d6daf03aa273dfcea3d93db09f2cf9`, logout using a predecessor cookie failed to revoke its already-rotated successor.
- Finding #18: `acf_6a6d52fc21f7c2f36761d5b62cddbecd`, GET logout allowed cross-site logout.

## Rotation and revocation

Session records now retain a `Successor` field. Rotation first turns the predecessor into a non-authenticating revocation alias, then saves the new credential. The database facade hides aliases from authentication. Their expiry remains the predecessor cookie's original expiry, so ordinary provider cleanup still bounds retention. Replacement expiry must not precede the original expiry; internal 30-day rolling sessions and OAuth's preserved recheck deadline meet this invariant.

Logout follows the persistent chain and deletes descendants before their predecessors. Thus a stale browser cookie can revoke one or multiple rotations, including after reopening the database. A storage delete failure cannot first remove the only path to a still-live descendant; retry can finish revocation. A separate login by the same user remains valid. Existing account/global session revocation also removes aliases.

The existing single-process session mutation lock serializes rotation and revocation for SQLite and Redis. This is not a multi-instance protocol or a guarantee that an already-authorized request is interrupted. A process/storage failure can still fail a request and require retry or login; no new distributed transaction is claimed. The existing provider error handling remains in use.

## Upgrade and rollback

SQLite schema 21 adds `Sessions.Successor`; Redis schema 13 uses the additive hash field. Both upgrades revoke existing sessions, because predecessor relationships from before this upgrade cannot be reconstructed. Expect users to sign in again. Passwords, API keys, file-sharing capabilities and stored files are unchanged.

Stop the old single instance before upgrading. Back up configuration and database under the normal procedure. **Do not run an older binary against the upgraded session store:** it does not understand non-authenticating aliases; SQLite also has an extra column. Roll back using an appropriate pre-upgrade store and revoke all restored sessions before serving traffic. No production upgrade or deletion was performed by this task.

## Verification

Tests cover SQLite and miniredis multi-generation logout, independent logins/users, persistent reopen, stale snapshot rejection, concurrent logout versus rotation, account/global revocation, migration and alias expiry, and injected deletion failure/retry. HTTP logout and client compatibility checks are listed in the final handoff. Miniredis is a test stand-in, not a real Redis deployment test.

No session management dashboard, new identity provider integration, automatic key rotation, multi-instance coordination or unrelated group D work is included.

## Browser logout

`GET /logout` (and other non-POST methods) now returns 405 without altering session state. The admin UI obtains a fresh, one-use browser-bound logout token from `/logoutToken` at click time, then POSTs it. This avoids an expired token in a tab left open for more than five minutes. Explicit foreign Origin/Referer or cross-site/same-site Fetch Metadata is rejected; older clients without those headers must still supply the browser-bound token. Rejected requests do not clear the session cookie or broadcast a logout. On success, the JS closes the shared SSE connection and navigates to login; native form requests use 303 rather than replaying POST with 307. OAuth retains its consent prompt.

Token issuance/logout deliberately do not require a currently authenticating session: the old cookie may already be a revocation alias. A group B Internal user awaiting mandatory password change can still perform this flow; logout does not clear the mandatory-change flag. External-authentication behavior is unchanged.

## Checks run on the PR #96 baseline

- `go test -count=1 -tags='test noaws' ./internal/configuration/database/... ./internal/webserver/authentication/... ./internal/webserver/api ./internal/authorization` passed.
- Database targeted race check: `go test -race -count=1 -tags='test noaws' ./internal/configuration/database -run 'TestSession(Lineage|Logout)|TestSEC021SessionMutationOrdering'` passed.
- Real registered HTTP routes: `go test -tags='test noaws' ./internal/webserver -run '^TestLogout' -count=1 -v` passed, including stale-cookie rotation revocation and mandatory-password-change compatibility.
- `node --test build/tests/logout.test.cjs` passed (three tests); JS browser primitives are mocked, not a browser visual test.
- Full `go generate ./...` and `go build -tags=noaws ./cmd/supashare` passed with `GOFLAGS=-buildvcs=false` for this cloud worktree. Generated WASM is an ignored build output; generated/minified tracked assets are included.

Initial verification exposed a retry-unsafe schema column addition and a legacy 2038 renewal fixture. The migration now tolerates a column already added before version writeback, and the session fixture uses a realistic one-hour expiry. A panic from that initial run left disposable test data; it was removed before the successful clean regression. An initial generation attempt failed at VCS stamping; the full supported generation was rerun successfully with buildvcs disabled. These failures are not presented as passing checks.

Not run: full webserver suite, actual browser visual/end-to-end testing, production deployment, real Redis failure/recovery or a real external identity provider. Local Codex should verify patch SHA and base, perform its review and required local UI checks, and create an ordinary PR only. No merge or next-group work is included in this handoff.
