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
