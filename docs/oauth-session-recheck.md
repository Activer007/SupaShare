# OAuth session recheck deadline

OAuth session IDs rotate after twelve hours of use, but rotation is **not** an
identity-provider or group-membership check. Replacement sessions and their
cookies now keep the predecessor's `ValidUntil`. Continued activity cannot keep
extending that stored deadline. After expiry the normal OAuth authentication
flow is required again; a successful login establishes a new deadline using the
configured recheck interval.

Password-login sessions retain their existing sliding thirty-day expiration.
The existing rotation/revocation synchronization is unchanged, and no database
migration or configuration change is required.

## Upgrade boundary

Existing sessions keep their currently stored expiration. Deadlines already
extended by an older release cannot be reconstructed from the session record.
The fix prevents further extensions; it does not retroactively recover the
original login time or automatically log out every user. If an operator needs
an immediate identity recheck, revoke the affected sessions through the existing
session-revocation mechanism. Changing the configured interval alone is not a
retroactive expiry change.

## Focused verification

`go test -tags 'test noaws' ./internal/webserver/authentication/sessionmanager`
includes synthetic temporary SQLite and in-process Redis tests for 1, 6, 12,
24, 72, 168, 336 and 720 hour intervals, repeated ID rotation, unchanged database
and cookie deadlines, rejection after expiry, a new login's deadline, and
unchanged password-session sliding expiration. Existing concurrent rotation
and revocation contracts remain part of this package. These checks do not use
real identity providers or production accounts; live-provider behavior and
real Redis deployment performance are outside their scope.
