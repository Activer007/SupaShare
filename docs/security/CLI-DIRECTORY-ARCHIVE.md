# CLI directory archive boundary

Addresses canonical scan row **index 3**, `acf_4ed64e45d19bd87a31f71fa946d8cb4e` (high): directory upload followed file symlinks and could include readable files outside the selected tree.

## Behavior

- `upload-dir` archives only regular files and directories. A source-root symlink, nested symlink (including an internal or dangling link), FIFO, socket, or other nonregular entry observed during validation fails the whole archive. Archive creation must succeed before uploading begins. Remove the link or deliberately copy its intended contents into the source first.
- Directory handles are anchored with Go `os.Root`. Each directory is checked against its pre-open identity; each file is opened relative to the pinned directory and checked with `f.Stat`/`os.SameFile` before creating its ZIP entry. Unix opens also request `O_NOFOLLOW | O_NONBLOCK`. `O_NONBLOCK` avoids blocking if a regular file is exchanged for a FIFO. Go's `os.Root` may resolve confined links even with `O_NOFOLLOW`, so confinement and post-open identity checks provide the root-escape and replacement boundary. Detected replacement returns an error.
- ZIP paths use `/` on every platform. Unix names containing a backslash are rejected rather than creating ambiguous extraction paths on Windows.
- Empty directories and normal nested files remain supported. If the temporary parent is inside the source tree, only the newly created temporary directory is excluded by filesystem identity. Unrelated user directories with similar names remain included.
- Temporary archives are created with mode `0600` (where POSIX modes apply). Failure removes the partial temporary directory. Both ZIP finalization and output-file close errors prevent returning an uploadable archive.

## Compatibility and limits

Uses `os.Root` from the repository's existing Go 1.26.10 toolchain; no dependency or toolchain upgrade. Native CLI targets Linux, macOS, and Windows retain support. JS and Plan 9 directory archiving fails closed because their `os.Root` implementations do not provide the required rename/race boundary.

This is not a filesystem snapshot or a general sandbox against a privileged/local owner. A regular file may change while being copied; stop concurrent writers when a consistent archive is required. Hard links and mounted directories are not symlinks and remain normal selected-tree contents. The fix does not hide content intentionally reachable through those mechanisms, defend against privileged mount changes, or limit archive size. Windows reparse behavior relies on `os.Root` and entry/handle type and identity validation; native Windows execution must be recorded separately from cross-compilation.

Direct single-file uploads, credential storage, redirects, and server behavior are unchanged.

## Verification

Regression tests use temporary directories and synthetic markers only; no server, API key, real user secrets, or production data is used. Coverage includes nested Unicode files, empty directories, own-temp exclusion, root/internal/external/dangling/directory links, file replacement, pinned-root rename, FIFO/socket rejection, nonblocking FIFO replacement, portable ZIP names and cleanup after failure. Deterministic fault injection also covers a child-directory replacement between inspection/open and underlying ZIP write/finalization errors. These tests exercise specific race boundaries, not an exhaustive schedule of concurrent mutations.

Run:

```sh
go test ./cmd/cli-uploader/... -count=1 -timeout=90s
go test -race ./cmd/cli-uploader -count=1 -timeout=90s
go vet ./cmd/cli-uploader/...
GOOS=windows GOARCH=amd64 go test -c ./cmd/cli-uploader -o /tmp/archive-windows.test.exe
GOOS=darwin GOARCH=arm64 go test -c ./cmd/cli-uploader -o /tmp/archive-darwin.test
```

### Cloud verification result

Linux focused CLI tests, race test and vet passed. Windows amd64 and macOS arm64 test binaries cross-compiled successfully; neither was run natively in this cloud environment. The Unix socket test explicitly skips only permission-denied socket creation because the cloud sandbox prohibits it; FIFO and other link/identity tests executed. No full server suite or live upload was performed for this CLI-only change.

### Local macOS verification result

On macOS amd64 with Go 1.26.10, the CLI-wide tests and race tests, CLI vet and a native binary build passed. The first socket regression failed because the default macOS temporary path plus the test name exceeded the Unix socket path limit. Its fixture now uses a short random directory under the OS temporary root. The final run executed the socket regression and every other test without skips.

The native CLI `upload-dir` entry point was also exercised with a loopback mock server, a temporary configuration and a synthetic key. A normal nested Unicode file and empty directory arrived as a valid ZIP, and successful output was cleaned up. An external symlink caused failure before any HTTP request, removed the partial output and preserved the synthetic outside marker.

Deterministic ZIP write/finalization fault injection verifies error propagation from `writeDirectoryArchive`. Cleanup is covered by the traversal-failure regression and review of the shared error cleanup defer in `zipFolder`. Output-file close failures and disk-full conditions were reviewed in code, without OS-level fault injection.

On this macOS host, Windows amd64 CLI build/vet and archive-test cross-compilation passed, as did macOS arm64 CLI build and archive-test cross-compilation. Native Windows and macOS arm64 execution remain unverified.
