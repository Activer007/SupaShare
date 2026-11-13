# File Request invitation boundary (security group A)

Base: a35485761a4bef2facb926dee14c625a2cfdb172 (PR #94).
Finding: acf_7722cce07f8f8355c6d1a3d8f4fd2c8b; CSV SupaShare item 9.

File Request `apikey` is an upload bearer capability, not collection metadata.
Management list, detail and edit responses now use a presentation-only copy.
Only the collection owner receives the invitation key. Cross-owner readers,
editors and administrators retain their existing metadata permissions but do
not receive the owner's upload capability. Rank alone does not grant this
secret. Non-authorized viewers still cannot read collection metadata.

HTML and dynamically created rows omit invitation/copy links without a key.
Persistence and public invitation verification are unchanged. Existing invitation
keys are NOT rotated or revoked by this update. If a link was exposed previously,
the owner/operator must assess and explicitly revoke/recreate that invitation;
this patch cannot undo knowledge of an already disclosed bearer credential.

Build through the repository's normal generation process (`go generate ./...`)
so generated/minified management JavaScript reflects the source change. The PR
includes the regenerated tracked assets and their content-hash references.
Generated WASM remains ignored by the repository. No new schema or migration.

Acceptance: owner sees/copies an invitation; cross-owner list/detail/edit and
HTML contain metadata but no invitation key; stored key remains unchanged.
Use synthetic users/invitations for checks. No production credentials needed.

Local integration adds `TestFileRequestInvitationHTTPAuthorization` through the
real API `Process` entry point and extends `TestFileRequestInvitationHTML` through
the authenticated page handler. These cover owner, cross-owner administrator,
reader, editor and unrelated users, the API-key management bit and live user
guest-upload grant, edit persistence, and preservation of stored invitations.
