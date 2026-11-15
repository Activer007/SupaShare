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

## 本地 macOS 集成验证（2026-11-15）

- 基线与最新 master 均为 `6183c1b283475dc57685882e56294cac1a0d3f3c`，PR #113 已合并。补丁为 50195 bytes，SHA256 为 `f747bc9b71b57628b6724b8f9748f819f59f7a09c72af175a2692bbeb0408672`，校验后无冲突应用。复用已有干净 worktree，版本沿用 2.18.0。
- 原生 macOS 15.7 / x86_64，Go 1.26.10 darwin/amd64。server 与 CLI 的生产 noaws 构建通过，`git diff --check` 通过。本轮数据、测试程序及二进制均位于独立 `/private/tmp/supashare-n07-qa` 目录。
- `TMPDIR=/private/tmp/supashare-n07-qa go test -race -tags=test,noaws ./internal/secretinput ./internal/environment/flagparser ./internal/configuration/database/migration ./cmd/cli-uploader/cliflags -count=1 -v` 通过，补充本次原生文件权限、终端不可用及参数解析证据。
- 独立临时 module 调用实际 flagparser / cliflags 入口，用 macOS `pty.openpty` 驱动管理员密码、upload / upload-dir / download 密码及迁移双提示。输入时原生 termios ECHO 关闭，输出无合成秘密；正常完成后所有终端状态恢复，仅归一化内核 PENDIN 位。
- 同一终端验证部分输入后 Ctrl-C、外部 SIGINT、SIGTERM、EOF 和空输入，均以失败退出并恢复终端状态，输出无部分合成秘密。上述证据来自 POSIX PTY，Windows console 原生行为仍待验证。
- 私有文件 CRLF 输入成功；首次解析后删除文件，再连续构造三次 Environment 成功。三个 CLI 模式的私有文件来源均成功，管理员及 CLI 旧 argv 来源仍成功并在 stderr 给出脱敏提醒。0600 输入文件添加实际 Darwin `everyone allow read` ACL 后读取失败，诊断保持通用错误；测试 ACL 随即移除。
- 真实生产 server 的迁移命令通过私有 URL 文件完成 SQLite → SQLite，核对源、目标数据库内合成用户一致，输出无连接 URL、临时路径或合成凭据。另覆盖不支持的源协议、畸形源 URL、不支持的目标协议及回环地址连接失败，均返回脱敏诊断。连接测试使用假凭据和本机回环地址。
- 云端 Linux、全仓与交叉编译结果直接复用。本地未重跑全仓，保留云端 TestTempDir 夹具及同基线 `TestAdminRenewalAuth` 失败记录。Windows 原生终端、UNC / junction / ACL / 替换验证仍待完成。GitHub Actions 当前 disabled，空 check-runs 无法提供 CI 通过证据。

## Auto Review 修复与回归

- 首轮审查 `7f57d262a7a0c245eb848f6f3fe8f301d504beed` 发现 P2，依赖的 `term.Terminal.ReadPassword` 编辑器达到 4096 字符后静默丢弃新增字符，可能接受与用户输入不同的秘密。
- 改为直接读取 raw 终端中的字节，显式执行 64 KiB 上限，支持 UTF-8 退格和 Ctrl-U 清行。Ctrl-C / Ctrl-D 取消；其他终端控制输入在本行结束后报错取消。超长及无效行在无回显状态下读至 Enter，再恢复终端并返回明确错误。交互行为已同步到 CLI 密码来源文档。
- 新增定向回归，5000 个 ASCII 字符、5000 个中文字符和恰好 64 KiB 输入完整返回，超限输入失败，UTF-8 退格、清行和取消输入通过。`secretinput` race 与 vet 通过。
- macOS 原生 PTY 复验全部原有提示和中断恢复场景，并补充 5000-byte、65536-byte 完整解析、65537-byte 显式拒绝、UTF-8 退格及不支持的终端控制输入。各场景均无合成秘密回显且终端状态恢复。修复后 server / CLI 再构建通过，Windows amd64 secretinput 测试包交叉编译通过。
