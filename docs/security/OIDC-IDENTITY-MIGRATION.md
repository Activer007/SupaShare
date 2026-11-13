# OIDC identity binding upgrade

OIDC login now identifies an account by the **exact configured issuer and case-sensitive subject (`sub`)**, not its email address. A verified email alone is never sufficient to claim an existing local account. Already-bound users keep their local ID, files, permissions and local account name when their provider email changes. New-account provisioning requires a verified email and respects `OnlyRegisteredUsers`; an email collision is rejected, not merged.

The database schema upgrade revokes **all existing browser sessions once**, including internal-auth sessions, to invalidate sessions minted under the previous identity policy. API credentials are not revoked automatically. SQLite schema becomes 20; Redis schema becomes 12.

## Existing installations: migrate before reopening login

Existing OIDC accounts are deliberately **not automatically linked**. Without an explicit binding their next OIDC login is denied. This includes the super administrator created during setup. First-time OIDC installations must also explicitly bind that initial administrator before use.

1. Schedule a maintenance window and stop **all** SupaShare instances using the database. Back up the configuration and database. Do not use a production database for tests.
2. In the trusted identity provider's administrative interface, independently verify the intended person's account and copy its immutable subject for this application. Confirm that the issuer exactly matches `Authentication.OAuthProvider`. Do not obtain the subject from an unverified token, emailed claim, or a login attempt that merely presents the same email address. Subject identifiers can be application-specific (pairwise).
3. Obtain the existing local numeric user ID from the administrator's user list or a trusted database backup. Confirm its owner and permissions. Link the super administrator first, then each existing user. Execute the upgraded binary under the normal service account with the deployment's same config/environment:

   ```sh
   ./supashare --config /path/to/config.json --oidc-link-user 1 --oidc-subject 'exact-case-sensitive-provider-subject'
   ```

   The command requires an existing OIDC configuration, upgrades/connects its configured SQLite or Redis database, binds the identity, revokes the selected user's browser sessions, logs an audit event, closes the database and exits. It does not start setup or an HTTP server. The subject is not a bearer credential, but command histories/process listings can retain it; apply normal local privacy controls. Audit output uses a SHA-256 fingerprint rather than the subject or email.
4. Keep the binding command's output with the operator's approved migration record. Never edit a conflicting identity binding to make a login succeed. Investigate the owner, issuer and subject first. Repeating the same binding is safe; assigning an already-bound identity to another user is rejected.
5. Restart the service and verify each migrated account against its expected local ID and permissions. Test both administrators and ordinary users. A different subject carrying the same email must be denied. Existing API credentials are not automatically rotated by the linking command; investigate and revoke them separately if compromise is suspected.

## Recovery and provider changes

A failed/unbound login is a security boundary, not a reason to turn on email auto-linking. An operator with trusted local deployment access can run the command even when no browser administrator can log in. Correct the identity evidence first. A changed issuer or pairwise subject represents a different identity; plan a verified operator migration rather than treating it as an email update. This command intentionally has no force-reassign option. Backups must include identity bindings, and any rollback to an older email-only authentication binary reintroduces the vulnerability.

The provider must issue ID tokens and support the standard validated OIDC flow. Upgrades can reject providers that previously worked with only a UserInfo response. Test the upgraded binary with a non-production provider/configuration before the maintenance window.

Do not lower the schema version or run an older binary against an upgraded store. If an operational rollback is required, stop all instances and restore the matching pre-upgrade configuration and database backup together; the old email-based policy is vulnerable again. A failed cross-provider database copy leaves its source intact and can leave a partial destination: discard that destination before retrying. Identity indexes must be consistent before copying; malformed or one-sided bindings are rejected rather than silently omitted.
