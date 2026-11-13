# Browser encrypted-download isolation

Finding: `acf_d8c6cc999d4c658249f2901709a0cb40` (canonical CSV index 5).

Decrypted file streams and key backups must never be transferred to a third-party
StreamSaver helper. Both previously configured external helpers have been removed.
The shared StreamSaver implementation rejects cross-origin helpers, uses exact
message target origins, and rejects cross-origin download URLs. Its bundled helper
accepts messages only from its same-origin parent/opener, including queued messages.

## Runtime behavior

- HTTPS and browser-trusted loopback/localhost contexts retain same-origin streaming
  through the bundled service worker. The browser's `isSecureContext`, not the
  administrator's public URL setting, determines the context. The application must
  be served in a compatible browser and service-worker registration must succeed.
- Remote HTTP remains available for development using a **local Blob fallback**.
  The UI explicitly warns about HTTP and the **64 MiB decrypted-byte limit**.
  Byte 67,108,865 is rejected before retaining that chunk, accumulated chunks are
  cleared, the read is cancelled, and no partial file is offered for saving.
  Large encrypted downloads require HTTPS. There is no external-helper fallback.
- Small encryption-key backups use a local Blob directly and release its object
  URL after initiating the download. Browser download initiation is not proof that
  the user actually saved the file; keep the backup before leaving setup.
- Existing secure Safari Blob fallback remains supported; it has the existing
  memory characteristics. Plain (non-client-decrypted) downloads are unchanged.

HTTP compatibility is not a security guarantee: a network attacker can modify an
HTTP page or script and steal data. Use HTTPS for sensitive files and production.
The 64 MiB bound limits the HTTP output accumulator, **not total browser memory**:
WASM decryption, response queues, transient chunks and Blob copies may use more.
The limit is enforced while reading because display size metadata is not a reliable
plaintext-byte bound for all encryption modes. A rejected download can therefore
already have consumed the server's download allowance. Reopen the link over HTTPS;
ask the owner to renew an exhausted link rather than retrying on HTTP.

## Verification

Run `node --test build/tests/*.test.cjs`. Security tests execute both readable and
production-minified StreamSaver assets with synthetic data: remote HTTP local save,
64 MiB boundary failure, abort cleanup, cross-origin helper rejection, HTTPS and
localhost same-origin messaging, helper origin/source filtering, and local key
backup behavior. These are isolated Node/browser-API mocks, not real-browser E2E.
Manual release checks should cover HTTPS/localhost streaming, remote HTTP small and
large files, Chromium/Firefox/Safari, proxy subpaths, cancellation, and WASM payloads.
No real user files, secrets, external helper requests or production deployment are
needed for these checks.

## Local review and verification

Local review reproduced a pending writer after an invalid helper download URL in
both the MessageChannel and transferable-stream paths, for source and minified
assets. Invalid or malformed URLs and helper aborts now error the output stream,
cancel pending writes and close both channel ports. Normal close waits for the
validated download URL before releasing the channel, including when a small
payload finishes before the helper is ready. Abort/retry and this close ordering
have regression coverage. Blob failures also clear the output accumulator.

The StreamSaver script and helper URL carry the generated asset version. The
repository's version/minification generators refreshed the shared resource hash
and bundled assets so an upgrade requests the patched resources.

On macOS amd64 with Go 1.26.10, 135 Node tests passed, including 26 StreamSaver
security tests. Both WASM modules built. Targeted Go download/template/UI and i18n
checks passed. Source and production-minified JavaScript run the same security
regressions.

Browser plugin not available. Playwright used installed Google Chrome
157.0.8103.78 with an isolated profile at 1414 x 900. Production templates, bundled
scripts, WASM decryption and the same-origin helper were exercised with synthetic
payloads and local fixture HTTP/auth responses. The remote-HTTP test hostname
resolved only to loopback and had `isSecureContext === false`.

Eight browser checks passed: remote HTTP small-file decryption, exactly 64 MiB,
rejection of byte 64 MiB+1 without a partial download, trusted-loopback streaming,
streaming under `/team`, invalid-helper error completion, plain download, and
local key-backup export. Successful file bytes were checked with SHA-256. Requests
were confined to the fixture origins. Page identity, rendered content and console
output were checked; overflow and injected invalid-URL errors were expected. The
fixture replaced dynamic favicon responses and the backup authentication endpoint.

After all eight assertions and context cleanup completed, automatic Chrome process
shutdown stalled. The isolated test process tree was terminated explicitly.
Firefox/Safari, native HTTPS/proxy deployments and the full application
authentication/storage/download-accounting flow remain unverified in this local
browser run. These checks use synthetic fixtures and cover the browser transfer
flow rather than a full deployment audit.
