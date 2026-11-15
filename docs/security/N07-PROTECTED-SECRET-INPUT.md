# N07: protected secret input

Base: `6183c1b283475dc57685882e56294cac1a0d3f3c` (PR #113).
Finding: CSV row 8, `acf_6b118522a657115c3a381b631abe34b9`.

## Scope and compatibility

- Uploader passwords: `--password-file` or `--password-prompt`.
- Deployment administrator password: `--deployment-password-file` or
  `--deployment-password-prompt`.
- Migration database locators: `--source-file` / `--destination-file` or
  `--source-prompt` / `--destination-prompt`.
- Old inline password/URL flags remain compatible, with a value-free stderr
  deprecation warning. **Choosing those old flags still exposes secrets in argv,
  shell history and service definitions. This patch cannot erase existing history.**
- New secret sources are mutually exclusive and cannot be repeated. Prompt
  selectors take no argument (server accepts explicit `=true`, not `=false`).
- Protected files reuse N06 ownership, directory, symlink and permission checks.
  Inputs are bounded to 64 KiB; one final LF/CRLF is removed, not arbitrary spaces.
  Explicit file/prompt input cannot be empty or contain embedded line breaks/NUL.
  Existing explicit empty inline values keep legacy semantics.
- Prompting requires a real terminal and does not silently fall back to echoed
  stdin. Prompt/warning output goes to stderr. Automated deployments should use
  protected files; generic stdin and environment-secret channels were not added.
- Repeated Environment construction parses nonsecret configuration only, without
  reopening a secret file or prompting again. Initial command parsing resolves it.
- Migration diagnostics omit connection locators and underlying error payloads,
  including malformed URL errors. This trades detailed error output for secrecy.
- File paths remain visible in argv. Do not put secrets in filenames. No changes
  to authentication semantics, stored credential format or quota policy.

## Validation

Linux cloud validation:

- `go test -race -tags=test,noaws,security_chunk` on secretinput, privatefile,
  flagparser, migration and the complete CLI uploader subtree passed.
- Parser child-process tests cover file input, legacy warning, conflicts,
  repeated sources, no terminal, empty/missing files, diagnostic redaction and
  Environment projections; CLI tests cover argument consumption/config scanning.
- Actual Linux PTY checks passed: ECHO disabled while typing, synthetic input not
  echoed, successful resolution restores terminal, SIGINT restores terminal.
- Production server `go build -tags=noaws ./cmd/supashare` and targeted vet passed.
- Complete `go test -p 1 -tags=test,noaws,security_chunk ./...` ran. It reported
  `TestTempDir` due to the deliberately relocated TMPDIR (environment package
  passed with TMPDIR unset), plus `TestAdminRenewalAuth: Session not renewed`.
  The latter independently fails on pristine base 6183c1b and is not marked fixed.
- Windows amd64 and Darwin arm64 secretinput test packages cross-compiled.
  **Cross-compilation is not native terminal/ACL execution.** Windows console
  behavior and native interactive macOS behavior remain for local acceptance;
  this does not replace the N06 platform filesystem validation record.

## Local Codex handoff

Apply this patch on an up-to-date branch containing PR #113. Check applicability
before applying; preserve unrelated work. Review all changes, run relevant tests,
then create an ordinary (non-draft) PR. Do not auto-merge and do not start another
finding group. Never paste real passwords/connection URLs into commands, logs,
PR descriptions or screenshots. Use synthetic credentials for native prompt
checks, including cancellation and terminal restoration. Record platform limits
and the baseline failure explicitly rather than claiming an all-green suite.
