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

Legacy ordinary, unencrypted records can expose `SizeBytes=0` for unknown size.
They retain the server's established read compatibility, using Content-Length
when available and the client byte cap through EOF otherwise. Collection and
encrypted metadata retain exact size checks.

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

### 本地 macOS 集成验证（2026-11-15）

- 最新 master 为 `156a3f293165f4a4979d3c87ad4837d75ded8fd8`，PR #114 已合并。联合补丁为 60047 bytes，SHA256 为 `c6d694be2a08ad2cb5704b096b310ed375b8a814e353e9e618432e118026db3f`，校验后无冲突应用。复用已有干净 worktree，版本沿用 2.18.0。
- 原生 macOS 15.7 / x86_64，Go 1.26.10 darwin/amd64。server / CLI 生产 noaws 构建、`git diff --check` 和 CLI 子树及 serviceurl / safedisplay 的 vet 通过。
- `TMPDIR=/private/tmp/supashare-n08-n09-qa go test -race -tags=test,noaws ./cmd/cli-uploader/... ./internal/serviceurl ./internal/safedisplay -count=1` 通过，补充 macOS 原生文件系统、网络与输出证据，包含替换目标名称后的失败清理回归。
- 使用构建后的原生 CLI 执行现有 `build/test-cli-secret-input.py`，19 个 POSIX PTY 场景通过。核对规范化目标展示及 yes 确认、欺骗 userinfo / query / fragment 在 API key 提示前拒绝、拒绝确认、API / E2E 隐藏输入及中断恢复。
- 启动独立真实 server、SQLite 与合成 E2E 账号，客户端实际上传 2097169-byte 内容，使用 1 MiB 分片，得到 2099233-byte 密文。真实 `/chunk/add`、`/chunk/complete`、E2E lease / get / set 与 DownloadFile 流程完成，下载明文 SHA256 与源一致，文件 mode 为 0600。
- 真实 E2E 下载入口拒绝已有文件、最终 symlink 和过小密文字节预算。对隔离数据中的密文末尾认证标签做同长度破坏后，下载认证失败、0600 partial 保留，`--remove` 未执行远端删除；恢复密文后重新下载并执行 `--remove` 成功，明文哈希一致，随后远端查询返回 404。
- 原生 CLI 配合两个回环 HTTP 服务验证简单反向代理 base path、控制面跨源重定向拒绝、存储下载跨源及返回原 origin 后 API key 均已移除。流式响应超出元数据长度时，磁盘仅写入预算内前缀，partial 保留且禁止远端删除。
- 回环服务返回带 OSC / BEL / Unicode 格式字符的元数据，原生 CLI 人类输出显示转义，JSON 解码后保留原始字段。全部账号、输入文件、服务和数据均属于 `/private/tmp/supashare-n08-n09-qa` 隔离验证环境。
- 30 分钟为分片／下载单请求的绝对期限，持续有进展也会到期。本地使用短时小文件，未实跑等待 30 分钟到期。普通下载沿用输出目录 ACL，本次 mode 验证不构成 N06 私密文件 ACL 验收。失败 partial 保留行为与恢复说明维持上文约定。
- 云端全仓及交叉编译结果直接复用，本地未重跑全仓。保留同基线 `TestAdminRenewalAuth` 与 TestTempDir 夹具记录。Windows 原生 console、UNC / junction / ACL / 文件替换仍待验证，真实 S3 运行及大文件／长时压测未执行。GitHub Actions 当前 disabled，空 check-runs 无法提供 CI 通过证据。

### Auto Review 修复与回归

- 首轮审查 `ba6da2cb5cbbb3056fb19763d6eceb0871c98556` 发现 P2，旧版普通未加密记录以 `SizeBytes=0` 表示未知大小，新精确比较会阻止其下载。已按服务端 `legacyUnknownSize` 范围保留兼容，使用响应 Content-Length 或客户端流量预算，同时保留 collection / encrypted 的精确检查。人类进度展示使用实际已知长度或未知长度模式。
- 新增有／无 Content-Length 的旧记录成功与超额拒绝回归，并核对 collection / encrypted 继续严格校验，失败保持 partial 且禁止远端删除。`TestDownload` 定向 race、cliapi vet 与生产 CLI 再构建通过。
- 真实 server 的合成旧记录保持 `SizeBytes=0`，修复后的原生 CLI JSON／人类模式下载均与内容一致；小预算拒绝并保留远端文件。共享下载路径改动后，完整 E2E 上传／下载／认证失败／安全删除，以及原生重定向、流量和终端输出夹具复验通过。
