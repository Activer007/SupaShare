// Package privatefile implements owner-private credential storage shared by the
// CLI and server. It never creates directories or repairs existing directory
// permissions. Callers must provision a private real path before writing.
//
// Existing symlinks (including ancestor links), nonregular files, foreign
// ownership and unsafe directory permissions are rejected. Write replaces an
// owned permissive regular file rather than modifying its old inode. Read
// requires private permissions. Random temporary files are created exclusively,
// secured before secret bytes are written, synced and closed before publication.
//
// Windows applies and verifies a protected DACL; directory crash durability is
// OS-dependent because portable directory fsync is unavailable. Unix syncs the
// containing directory after publication. An error from that final sync may
// mean the new file has already been published. Darwin inherited ACL grants are
// checked and rejected, not silently removed. On other supported Unix platforms
// the existing ownership/mode policy is retained. Unsupported platforms fail
// closed. Native Windows ACL/rename and Darwin ACL behavior require platform
// tests; cross compilation alone is not runtime evidence.
package privatefile
