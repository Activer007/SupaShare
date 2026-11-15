# N08 + N09: CLI service and transfer boundaries

Base: `156a3f293165f4a4979d3c87ad4837d75ded8fd8` (PR #114).

One CLI-focused batch, covering new CSV rows 4, 23, 5 and 9 respectively:

- `acf_ceee6f85f8ec971325b5661e363a1509`: deceptive service URL.
- `acf_8e769faa9a962dcd9f8cd897b1b86547`: terminal control characters.
- `acf_95aca051ec60e8dafec0da42167e9611`: server-controlled transfer resources.
- `acf_1944c92eac70e0698e5401ced6a82b87`: download destination race.

## Changes and operational impact

Login parses the URL before requesting an API key, shows its canonical service
base, and requires `yes`. HTTP remains supported with a warning that it has no
transport encryption. Userinfo, query, fragment, ambiguous/encoded paths and
non-ASCII host forms are refused. Use explicit ASCII IDNA for international
hosts. Simple reverse-proxy base paths remain supported. Login's historical
trailing `/admin` convenience is displayed before confirmation; loaded settings
never silently remove this segment. Save, load and API initialization validate
again. Invalid initialization clears prior credential state. DNS ownership is
not certified by URL parsing: verify the displayed destination yourself.

Human output visibly escapes controls, Unicode format characters and malformed
UTF-8, and bounds each remote text field to 2,048 bytes. Successful JSON output
uses encoding/json and preserves the original metadata. Chunk error bodies are
not echoed. This is not a server audit-log change (N10 remains separate).

Uploads stream multipart bodies instead of buffering whole chunks. Advertised
chunk sizes must be 1–64 MiB and file limits must be positive and overflow-safe.
Default server chunks are 45 MiB. Installations with larger custom chunks must
lower the server chunk setting to use this CLI. Metadata responses are limited
to 8 MiB, chunk replies to 64 KiB and lease replies to 4 KiB. Large E2E indexes
may now be rejected explicitly rather than allocated without a bound.

Chunk uploads and downloads have a 30-minute absolute request deadline. Normal
metadata retains 30 seconds; completion retains 30 minutes. These are finite
absolute budgets, **not an idle-progress timeout that extends indefinitely**.
Very slow or very large legitimate transfers can therefore fail at 30 minutes.
Per-progress tuning remains a possible follow-up, not claimed implemented here.

Downloads default to a 10 GiB client byte cap. Override deliberately with
`--max-download-bytes POSITIVE_INTEGER` (bytes). This is a local resource limit,
not a change to server quotas. Metadata is checked before creating a file;
streaming checks enforce exact advertised wire size, detect short/long bodies,
and bound disk output. For E2E, the API SizeBytes is ciphertext size: both wire
and plaintext are capped, and the decryptor must authenticate EOF. Exact clear
size is not asserted because the API does not expose it as a numeric field.

The output directory is pinned with os.Root and the final component is created
exclusively, mode 0600, without overwriting a regular file or following a leaf
symlink. This does not impose N06 secret-file ACL policy on ordinary downloads;
Windows access depends on the output directory's ACL. Choose a private directory
for sensitive downloads. A failed download **may leave a partial file occupying
the selected name**. The CLI deliberately never removes that pathname on failure:
a concurrent directory writer may have replaced it. Verify before deleting a
partial, or retry with a new name. Success requires EOF validation, Sync and
Close before `--remove` can delete the remote file. A local directory writer may
still rename/delete names; pinned-descriptor writes do not grant directory trust.

## Acceptance and local handoff

Run the CLI subtree and serviceurl/safedisplay tests with race enabled, vet,
build the CLI, and run `build/test-cli-secret-input.py` with the built CLI and
server binaries. Regression tests include URL redirection boundaries, terminal
escaping versus JSON preservation, bounded chunk responses, short streaming
sources, hostile chunk settings, exact/short/oversized downloads, existing files,
symlink leaves and replaced filenames on failure. E2E authentication and byte
accounting were tested at the decrypt-reader level, not as a complete encrypted
DownloadFile route acceptance test.

Create an ordinary PR after review; do not auto-merge. Preserve the existing N07
hidden-input protections and native-platform validation notes. Do not expand
this patch into N10 or unrelated server changes. Native Windows/macOS filesystem
and terminal behavior must be recorded separately from cross-compilation.

### Cloud verification record

- Complete CLI subtree plus serviceurl/safedisplay: race tests and vet passed.
- Real Linux PTY harness: 19 scenarios passed, including pre-secret destination
  rejection/decline and prior hidden-input interruption/restoration coverage.
- CLI binaries built for Linux, Windows amd64 and Darwin arm64. The latter two
  are cross-builds, not native filesystem/terminal acceptance.
- Full repository tagged test run performed. `TestTempDir` fails with this
  executor's workspace TMPDIR; environment tests pass with TMPDIR unset.
  `TestAdminRenewalAuth: Session not renewed` independently reproduces on pristine
  base 156a3f2. Neither unrelated issue is claimed fixed by this batch.
