# CLI file-password input without secret argv values (N07)

The `upload`, `upload-dir`, and `download` commands retain their existing
`FlagConfig.Password` semantics. This change only changes how that value reaches
the command; it does not add server permissions, rotate passwords or change
credential login storage.

Prefer one of:

```sh
supashare-cli upload -f report.pdf --password-prompt
supashare-cli upload-dir -D reports --password-file /private/secrets/file-password
supashare-cli download -i SHARE_ID --password-file /private/secrets/file-password
```

`--password-file=PATH` is also supported. Provision the file using your secret
manager or a trusted editor, not an `echo` command containing a real password in
shell history. The file and its real parent directory must meet `privatefile`
security checks: owner-private regular file (normally Unix 0600), safe non-shared
parents and no symlinks, or a private Windows ACL. Existing permissions are not
automatically repaired. A bad path, unsafe file, or failed read aborts with a
generic error that does not include the path or secret.

Files are read with a 64 KiB byte cap. At most one trailing LF or CRLF is removed;
spaces are preserved. Empty file/prompt values, NUL, CR and embedded newlines are
rejected. `--password-prompt` requires a real terminal and has no echoed or piped
fallback. Prompt output and inline warnings go to stderr, keeping JSON stdout
clean. Raw terminal state is restored on normal return and supported
interrupt/termination paths. SIGKILL and compromised terminals are outside this
protection. There is no new stdin or environment-variable secret source.

Legacy `-p VALUE` / `--password VALUE` remains compatible, including an explicitly
empty password; it emits a warning without printing the value. `-p=VALUE` and
`--password=VALUE` are recognized too. Inline arguments can still appear in shell
history and process listings, so this is compatibility, not safe transport.
Multiple sources and repeated source options are rejected before reading a file
or prompting. Help does not resolve secret sources. Consumed option values are
not reinterpreted as further flags.

## Checks and limits

Run:

```sh
go test -race ./internal/secretinput ./internal/privatefile ./cmd/cli-uploader/...
go vet ./internal/secretinput ./cmd/cli-uploader/cliflags
```

Tests cover source conflicts, empty legacy compatibility, malformed and oversized
files, private permissions, links, opaque errors, non-TTY rejection without
consumption, and all three CLI command modes. Native Windows/macOS terminal
behavior needs separate platform verification; cross compilation is not native
runtime evidence. This does not encrypt secrets in process memory or remedy old
history/process-list exposure.
