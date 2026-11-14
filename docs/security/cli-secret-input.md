# CLI hidden secret input (group F)

Scope: finding `acf_1a645484567f85ae7c86d08cffb76a05`, original CSV
index 14 (the fifteenth SupaShare finding). This is separate from credential-file
protection and archive/redirect fixes. Base: `8e072e07c687adef0c83feb786e76eeefaf7c6aa`
(PR #99, source version 2.18.0). No version/tag/release or server change is included.

## Behavior

- Both `login` API-key and E2E-key prompts use a local terminal-only no-echo
  reader, then move to the next line. No plaintext, masking stars or partial
  key is printed. The server URL remains ordinary visible input.
- Use `golang.org/x/term`'s raw terminal and password reader. This also lets
  Ctrl-C / empty Ctrl-D cancel rather than accepting a partial entry. Raw
  terminal state is restored on return, input failure, Ctrl-C, and SIGTERM.
- Secure terminal setup/read failure aborts; errors do not include input or
  backend error payloads. In particular, do not use the existing global
  `helper.ReadPassword`, which falls back to visible `ReadLine` on failure.
  Other callers of that global helper are outside this narrow finding.
- Failed or cancelled login does not save or replace the existing credential
  file. Normal valid API and E2E logins retain connection checking, key
  validation and the existing private credential-store policy.
- Redirected/piped login is now explicitly rejected, including CI stdin.
  Automation may use an already provisioned private `-c` configuration with
  existing upload/download commands. No implicit stdin, environment variable,
  command-line key option or new credential import path is added. Docker login
  needs `-it`. Windows needs a console supported by `x/term`.

No protection is claimed against keyloggers, a compromised terminal, clipboard
capture, input-recording software, SIGKILL, or already leaked keys. Paste only
when the matching hidden prompt appears, not together with the visible URL.
Existing exposed keys require separate authorized revocation/rotation. No real
credentials are used or rotated by this change. This is not memory encryption.

## Reproducible checks

```sh
go test ./cmd/cli-uploader/... -count=1
go test -race ./cmd/cli-uploader/... -count=1
go vet ./cmd/cli-uploader/...
go build -o /tmp/supashare-cli ./cmd/cli-uploader
python3 build/test-cli-secret-input.py /tmp/supashare-cli
```

The Python test is POSIX-only, uses a disposable PTY and loopback mock API, and
requires Python 3.9+. It verifies API-only/repeated/E2E successful login, saved
synthetic values and private mode, hidden input, terminal-state restoration,
empty/malformed input, EOF, Ctrl-C, SIGTERM and rejected redirected stdin. Failure
paths preserve the prior credential file. It never reads real user configuration
or sends real credentials. Go tests cover nonterminal/closed input failing
closed, and nonterminal input remaining unread.

Cloud results are recorded in the handoff. A real macOS/Windows terminal, Docker
interactive console, and real remote server remain separate validation. No CI
configuration or database schema is changed; the broader webserver suite is not
relevant evidence for these CLI-only changes.

Cloud verification on the supplied patch (Linux, Go 1.26.10): CLI subtree tests,
the same subtree with `-race`, and `go vet` passed. The built CLI passed all 15
PTY/redirected-input scenarios above, including partial API/E2E input followed by
Ctrl-C and SIGTERM at both prompts. Windows amd64 and Darwin arm64 CLI binaries
cross-built successfully; that is compile evidence only, not native execution.
No server tests or full repository suite were run for this CLI-only patch.
