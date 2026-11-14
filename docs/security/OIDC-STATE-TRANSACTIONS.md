# Group D: OIDC state and browser correlation

Baseline: `75cd839f2794648099cb99c4fc02217cd48031bb` (PR #97 merged).
Finding: `acf_b87c1dfc457f12c65b05d2bfa04a15d9` (#10 in the SupaShare finding list).

## Boundary and implementation

The old generic `state` cookie and query comparison was vulnerable to sibling-domain cookie planting. Login now creates an unpredictable state, a separate browser binding, PKCE verifier and nonce in a bounded server-side transaction. The callback requires exactly one nonempty correlation cookie and one state parameter, then atomically consumes the matching transaction before processing provider errors or contacting the token endpoint. A callback from another browser does not consume the original browser's transaction. Matched expired attempts are consumed and rejected.

HTTPS deployments use `__Host-supashare-oidc`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no Domain. The browser's host-prefix rules prevent a sibling origin from planting this cookie. Legacy `state` cookies are ignored. Duplicate correlation cookies fail closed. The public configured URL, not arbitrary forwarded headers, determines transport policy. Subpath deployments retain callback coverage with `Path=/`.

HTTP OIDC is permitted only for literal loopback addresses or `localhost` development. A public HTTP URL now receives 503 on login/callback and must be changed to HTTPS. This is intentional: an unprefixed HTTP cookie cannot provide the sibling-domain isolation guarantee. Normal internal/header authentication is not changed.

Token exchange sends S256 PKCE's verifier, and the signed ID token nonce must exactly match the transaction nonce before UserInfo or account/session creation. ID token signature/issuer/audience/subject verification remains in place. Providers must support S256 PKCE and echo the OIDC nonce. No downgrade switch is added.

Callbacks clear the correlation cookie on terminal paths of a matched transaction (including expiry). Malformed, forged or stale callbacks do not clear an unrelated newer browser binding. `login_required` / `interaction_required` / `consent_required` consume the old attempt, then issue a new transaction and cookie for the interactive retry. This preserves the post-logout consent flow from group C. Callback errors do not destroy an already authenticated session.

## Lifetime and operational limits

Transactions are deliberately process-local, not persistent database rows: at most 4096 pending attempts, ten-minute lifetime, expiry pruning on insertion, mutex-protected single consumption. At capacity login fails with 503 rather than evicting a valid attempt or growing memory unboundedly. A process restart loses pending attempts; users restart login. Existing authenticated sessions are not cleared, and there is no database migration.

This is a single-process/sticky-routing implementation, consistent with the current process-local CSRF model. A callback on another process fails closed and requires a new login; this is **not** a shared multi-instance transaction service. With multiple login tabs the newest correlation cookie wins; older attempts fail and expire. It does not introduce general rate limiting.

Rollback: no schema change. Reverting code reopens the original cookie vulnerability and is not recommended. Pending authorization attempts from the old version must be restarted after upgrade. Passwords, API keys and stored authenticated sessions are unchanged.

## Validation and handoff

Cloud validation:
- OAuth package: host-cookie and subpath attributes, independent binding, PKCE challenge, nonce validation, missing/empty/forged/duplicate/replayed state/cookies, browser mismatch, provider error consumption, expiry, capacity, concurrent single consumption and HTTPS/loopback policy.
- Real HTTP proxy fixture: HTTPS terminates at proxy, inconsistent forwarding headers cannot change cookies; fresh login visits the fixture authorization endpoint, which checks PKCE at token exchange and signs the nonce. Both HTTPS proxy and loopback HTTP development flows establish the correct account session.
- Existing identity, signature, audience, issuer and callback deadline tests remain effective using issued transactions, rather than failing early on invalid test state.
- Authentication subtree and API subtree regression.
- OAuth race run and noaws application build.

The fixture is not a browser or external identity provider. Actual browser prefix enforcement, a real IdP, multi-instance routing and production configuration/migration are not verified here. Full webserver/storage suites were not required or claimed. GitHub Actions remain disabled; do not enable CI.

Local Codex: verify patch SHA and baseline, apply in an isolated worktree, review these boundaries, run proportionate checks and create an ordinary PR. Do not merge or start group E without the user's next direction.

Commands actually passed on the final implementation (Go 1.26.10):

```sh
go test -tags='test noaws' ./internal/webserver/authentication/... ./internal/webserver/api/...
go test -race -tags='test noaws' ./internal/webserver/authentication/oauth
go test -tags='test noaws' ./internal/test/securityfixture -run 'Test.*(OIDC|Identity|IDP)' -count=1
GOFLAGS=-buildvcs=false go generate ./...
go build -buildvcs=false -tags=noaws -o <output> ./cmd/supashare
git diff --check
```

Generated tracked assets did not change. `buildvcs=false` is used for this cloud worktree environment; normal local Codex generation should retain the project's usual metadata workflow. Build work/temp directories were scoped to the workspace, not the small system temporary filesystem.
