# CLI credential file hardening

Scope: canonical finding `acf_b1f8dff0d039389fb861c6da09ba41f4` (CSV index 2).

## Invariants

- No implicit current-directory or global configuration lookup. Native login uses the OS user-config directory under `supashare`; Docker and explicit `-c` remain supported.
- Credential entries must be regular, owned files. Symlinks (including dangling links), devices, and insecure parent paths fail closed. Ancestors are checked and an `os.Root` pins the parent for filesystem operations.
- Writes use a random exclusive-created temporary file in the same directory, secure it before serialization bytes are written, sync and close it, then replace the target with rename. No truncate/write of the old inode, no delete-old-file fallback on rename failure. A hard link to the old inode cannot receive the new credentials.
- Unix creates files 0600 and native directories 0700, checks effective UID (root-owned ancestors allowed) and rejects writable-by-others directories. Sticky ancestors such as `/tmp` are allowed, not a shared credential directory itself. Existing unsafe file permissions are repaired by replacement, not path-based chmod.
- macOS additionally reads the kernel's extended-security attribute. Directories reject unsafe allow ACEs, including inherit-only readers, before temporary-file creation. Ancestors reject replacement or ACL-changing grants. Reads and the new file descriptor reject unsafe file ACLs, even when the mode is 0600. Deny-only ACLs remain supported. The policy conservatively rejects dangerous allow ACEs without resolving GUID group membership, so custom managed ACLs can require a different private location.
- Windows checks owner and ACL, trusting the current user, SYSTEM and Administrators; ancestor ownership may also belong to TrustedInstaller. Ancestor replacement rights and immediate-parent read/write grants to other principals are rejected. This includes immediate-parent inherit-only ACEs, so a new temporary file cannot inherit an other-readable ACL during the pre-hardening interval. Files receive a protected DACL for the current user, SYSTEM and Administrators. Unknown ACE types fail closed. This may reject custom/enterprise ACLs: use a dedicated private user directory rather than disabling the checks.
- Reads reject unsafe credentials before loading a server URL/key, and check file identity after opening. Logout uses the same checked parent and never traverses a final link.

## Compatibility and limitations

See `docs/advanced.rst` for paths and migration. Old files are not automatically copied/deleted; re-login is preferred. Already exposed keys need rotation; this patch cannot revoke previous disclosure. There is no encryption at rest and no defense against administrators, another process running as the same user, or a filesystem that lies about permissions/atomic replacement. Symlinked credential directory components are deliberately rejected. Secure credential storage on other operating systems fails closed.

## Verification

Cloud validation of the supplied patch on Linux passed focused CLI tests with the race detector and `go vet ./cmd/cli-uploader/...`. Regressions cover new private defaults, explicit/Docker selection, malicious CWD files, regular/symlink/dangling/directory/FIFO handling, unsafe directory/owner, unsafe file read, atomic permission repair and hard-link preservation, and preserving an existing file on a rejected write. All credentials are synthetic and all test data are temporary.

Windows amd64 and macOS arm64 were cross-built in the cloud, and Windows-specific ACL regression tests were cross-compiled. On the local macOS host, cross-build and vet targeting Windows amd64, plus ACL-test cross-compilation, passed. The final local changes also passed cross-builds targeting Linux amd64 and macOS arm64, plus Linux amd64 vet. Native Windows execution remains unverified. A separate Windows runner could not materialize the patch because its Python lacked `os.setxattr`, and no Go toolchain was found. The default AppData path, inherited ACLs, repeated save, failure preservation and symlink/hardlink runtime checks were all unexecuted there.

Local macOS validation found that `os.UserConfigDir` follows the native Application Support path regardless of `XDG_CONFIG_HOME`. The default-selection regression now checks the OS-selected path without accessing real user credentials, then tests private-directory creation with an isolated cached path. Native Darwin ACL regressions exercise inherited readers, ancestor replacement grants, private replacement of an unsafe old inode, descriptor checks before content writes and safe deny-only ACLs.

On macOS amd64 with Go 1.26.10, the final code passed `go test ./cmd/cli-uploader/... -count=1`, the same suite with `-race`, `go vet ./cmd/cli-uploader/...`, and a CLI binary build. The binary's login/logout entry points passed isolated checks using a loopback mock server and synthetic keys. These verified explicit-path login with mode 0600, repeated login replacing an unsafe 0644 file while preserving the old hard link, rejection of an unsafe parent while preserving the previous file, symlink rejection, explicit logout, current-directory sentinel preservation and temporary-file cleanup. The default native path was selected in tests, with writes redirected to temporary test storage.

No production configuration, credential, service, unrelated security finding, CI setting, or dependency version is changed.
