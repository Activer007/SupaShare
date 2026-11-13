# CLI redirect security

The CLI now applies an explicit redirect policy to all authenticated HTTP clients.

- API calls (including upload chunks, completion metadata and E2E metadata) may
  follow same-origin redirects only. Cross-origin redirects fail before sending
  the next request, including its body. Configure the final canonical SupaShare
  URL directly if a proxy redirects to a different host, port or scheme.
- File downloads may follow redirects to presigned object-storage URLs. The URL
  and its signature query are preserved, but **all inherited request headers**
  are removed after leaving the API origin. Credentials are not restored if a
  later redirect returns to the API origin. Storage authenticates using its
  presigned URL, not the SupaShare API key.
- An origin is the scheme, case-insensitive hostname and effective port (80/443
  when omitted). Subdomains and different ports are not trusted automatically.
- HTTPS-to-HTTP redirects are rejected, including later hops. Redirect URLs
  containing user credentials or non-HTTP(S) schemes are rejected. The standard
  ten-redirect limit and existing request timeouts are retained.

An explicitly configured initial HTTP server URL is still supported for existing
local deployments; this does **not** protect its initial API key from network
observers. Use HTTPS for remote servers and object storage. A storage endpoint
that redirects HTTPS downloads to HTTP must be corrected rather than bypassed.

Regression tests use synthetic keys and local HTTP/TLS servers, not real API
credentials or S3 services:

```sh
go test ./cmd/cli-uploader/cliapi -count=1
go test -race ./cmd/cli-uploader/cliapi -count=1
```

This addresses security finding `acf_02747315bece43421d75cd61a7c219f9`
(original CSV index 4). It does not change server-side S3 URL signing or E2E
file decryption. Existing exposed API keys should be revoked and replaced by
an administrator; upgrading the CLI cannot revoke keys already copied into
storage/proxy logs.
