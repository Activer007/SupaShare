# Password reset and login work boundary (security group B)

Base: 39450d6 (PR #95, group A already merged).
Findings: CSV SupaShare item 12 `acf_18e0df868c3c7bd444875d4917fea54a`
and item 17 `acf_541b2007cede28ba3cd63222a165af2b`.

## Required password change is a restricted session

In internal-password authentication mode, the default administrative force-change action deliberately keeps the existing
password, sets ResetPassword and removes sessions. It is not credential rotation.
That password may open a new session **only to finish the password change**.
UI management pages redirect to the change form. Non-UI session endpoints
(token issuance, chunk upload, upload status and authenticated presigned download)
return 403 JSON before their handler executes. Password change with browser-bound
CSRF and logout remain available. Successful change revokes sessions and requires
a fresh login, as before.

Management API credentials belonging to a reset-required user are denied on live
user lookup, including keys issued before the reset. They resume working after
password change: this operation suspends use, it does not rotate/revoke API keys.
An authorized administrator can still inspect/edit/revoke a reset user's target
keys. The target lookup is not authentication as that user.

Public download/share links, Delivery capabilities and File Request bearer
invitations remain independent of the owner's password-reset status. This fix
must not silently revoke public capabilities. To contain a known compromised
password/key/link, the operator must explicitly rotate or revoke the appropriate
credential/capability as well; forcing password change alone is not full incident
containment. Historical local reset flags do not impose password-change confinement in OAuth,
proxy-header or disabled authentication modes, matching the previous UI policy.
No schema, migration or UI changes.

## Login work

Every CSRF-valid local login attempt performs Argon2id verification: a missing
username uses a valid dummy encoding and can never authenticate. Legacy SHA1,
empty and malformed stored hashes also pay the Argon2 work before their existing
verification behavior. Legacy successful logins still migrate normally. CSRF
validation and existing rate limits remain ahead of/around this work.

This removes the cheap missing-user/legacy failure path; it is not a claim of
constant end-to-end response time. Database, scheduling, legacy success migration
and deployment load can still vary. Tests deliberately avoid flaky wall-clock
thresholds and instead cover the shared work helper and response/auth outcomes.

## Verification

- Real registered HTTP routes: default admin reset, old session invalidation,
  retained-password fresh login, token/upload/SSE/presigned-download confinement,
  UI redirect, password form, rejected CSRF, successful change, fresh login and
  restored token/API access, including logout while reset-required.
- Existing management key denial, independent invitation key preservation and
  authorized administrator revocation of a reset user's key.
- Token generation denies reset-required users at all ranks.
- Missing/known invalid username outcome, modern/legacy/malformed hash paths and
  normal successful authentication. Existing onboarding collection-permission
  fixture now explicitly represents a user after mandatory password change.

Run the Go packages with `-tags=test,noaws`; use repository-generated embedded
WASM assets before compiling webserver tests. No production credentials or
production requests are involved. GitHub Actions are disabled by maintainer;
local results must be reported independently of CI.

Cloud verification on this patch: authentication subtree, authorization package
and API package passed with `-tags=test,noaws`; selected webserver login/admin,
password-change, File Request HTML and new reset HTTP tests passed. `go build
-buildvcs=false -tags=noaws ./cmd/supashare` passed (VCS stamping unavailable in
this worktree environment). Unchanged embedded WASM artifacts were reused from
the prior cloud build; no browser visual test or full webserver suite was run.
Local Codex should regenerate assets using the ordinary repository workflow
before its review and ordinary PR creation. Do not merge or start group C without
separate user instruction.

Local integration reused the cloud results and added two focused regressions:
`TestResetRequiredManagementAPIAuthModes` checks the live management API gate in
internal, OAuth, proxy-header and disabled modes. `TestPasswordResetPreservesPublicHTTP`
checks registered share/download/Delivery/invitation routes with a reset-required
owner and verifies that invitation API authentication reaches parameter parsing.
Both passed with `-tags=test,noaws`. The invitation request deliberately omits a
required parameter and does not upload a file. `go generate ./...` completed with
no tracked asset changes. Real IdP/proxy deployment and timing benchmarks remain
outside this verification.
