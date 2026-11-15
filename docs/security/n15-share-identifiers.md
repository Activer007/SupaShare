# N15：分享标识强度与创建冲突保护

对应新 CSV 第 16 项（原序号 15），finding `acf_a559ff73d395cb890cbda81b431f1192`。
基线：PR #108 后的 `a34379382f485620034bdfb4fbd9b2b1798de950`。

## 修改

- 新下载和 File Request ID 默认、最小均为 22 个均匀随机字母数字字符；热链仍默认 40，最小提升到 22。22×log2(62) 约 131 bit。随机源沿用 Go `crypto/rand`，没有时间戳、计数器或可预测回退。
- 配置低于最小值时提升到 22。读取入口仍接受历史 5 字符 ID；不批量改写、吊销或延长旧链接，旧链接的既有枚举风险不会因升级自动消失。敏感旧链接应由所有者另行撤销/重建，或加密码及有限有效期。
- 旧文件创建入口使用数据库原子 create-only 写入文件及可选热链，不再覆盖已有同 ID 元数据。碰撞最多重试五次，重新生成 ID/热链；其他存储错误不重试，不假报成功。
- SQLite 在同一写事务内检查并 INSERT，Redis 在单 Lua 内检查键类型/存在性后创建。配额上传、分块完成与复制已有原子碰撞拒绝，保留其事务和幂等契约，不在提交结果未知时盲目重放。
- 编辑时补建热链改用不可覆盖 claim，允许同目标重放，不允许改写他人的热链。失败返回冲突而不是成功；普通 SaveMetaData/SaveHotlink 仍供已授权编辑和离线迁移使用，不是创建接口。
- 文件名路由的无效 ID 和 File Request 拒绝已由 N16 修正。本组补热链无效/收集文件的同一失败 ID 限流，保留过期图片响应，不暴露内部路径。

## 与 N13 的叠加

N13 负责 File Request 与邀请凭证的原子创建、配额和生命周期；其 `New` 使用本组提高后的配置长度。本组不改 File Request 保存路径、不引入 schema 版本，避免抢占 N13 迁移。两个 PR 应分别审查，并对叠加树验证；不得将本组表述为已独立交付 N13 的请求原子性。

## 验证范围

- 新长度下限/历史读取下限；普通新 ID/热链形状。
- SQLite、Redis 模拟器同 ID 和同热链并发只允许一个胜者；旧短 ID/映射不变、冲突无部分写入、同目标 claim 幂等。
- 旧文件创建强制首次碰撞后重试，已有记录保持不变。
- 既有配额提交热链碰撞及重放回归。

Redis 模拟器不是实际 Redis；本地 Codex 仍应运行真实 Redis 与浏览器新分享/旧短链接访问、编辑补热链专项。没有生产数据操作或旧链接迁移。随机样本测试只验证格式与实现路径，不是熵的经验性证明。已发布的文件传输、其他安全分组及跨实例授权竞态不在本组范围。

## 本地集成验收（2026-11-15）

官方补丁为 28,032 字节，SHA256 为 `1431c08e0a0fc83912ad3f7585c544f11fe11883215bff4ded95a901dc1e199a`，本地文件核验匹配。复用已合并 N16 所在的干净 worktree，创建 `codex/n15-share-identifiers`，基线为 `a34379382f485620034bdfb4fbd9b2b1798de950`。N13 PR #109 的 HEAD 为 `5f02b6818c1439323f397313f7078e7fc2004d81`，N15 分支直接从 master 开始，提交范围仅有本组。

### 本地兼容修复

临时组合树复现了 N15 的 Redis 旧创建路径漏写 N13 关联文件索引。文件元数据已创建，但收集文件列表仍只有原文件。N15 现在会检测已安装的 `frqidx:version` 标记，先校验版本和文件索引类型，再在同一个 Lua 中发布文件、可选热链、修订号与关联成员。独立 N15 部署的标记为空时按现有结构创建。该实现只维护已安装索引，数据库升级由 N13 负责。

新增回归测试先在原实现失败，修复后通过，并覆盖错误索引类型导致的失败不能留下文件、热链或修订号部分写入。只重新执行受改动影响的 Redis N15 定向 race 测试，SQLite 与其他已核实自动测试复用云端结果。

### 真实 Redis 与浏览器

- 独立 Redis 7 容器仅绑定 `127.0.0.1:56395`，独立前缀、AOF 与临时文件。独立 N15 数据库版本保持 15，合成旧数据包括三个五字符文件 ID、旧图片热链和短 ID 的 File Request。
- 真实 Redis 验证已有 ID、已有热链冲突拒绝，旧元数据和映射保持，失败创建没有留下文件行，同目标热链 claim 可重放。两个 provider 各参与十二路竞争，同 ID 和同热链场景各只有一个胜者，失败方没有部分文件元数据。
- 真实 server 使用低配置 `GOKAPI_LENGTH_ID=5`、`GOKAPI_LENGTH_HOTLINK_ID=8`。Chrome 隔离管理 context 通过普通文件输入上传文本和 PNG 两个合成文件，实际队列均完成，生成的文件 ID 为 22 字符，图片热链主体为 22 字符。独立 API 新建 File Request 的 ID 同样为 22 字符。
- 历史五字符下载页和文件内容可读取，旧图片热链仍返回 PNG，旧邀请页仍打开。浏览器经“更多→编辑→保存修改”给旧图片补建 22 字符热链，新热链返回 PNG，原有另一份旧热链保持原映射。
- 真实公开浏览器 context 打开历史短下载页和新 22 字符下载页，文件名及下载入口正常。管理页与公开页控制台均无 warning/error，已截图核对新分享页面。普通上传的合成 File 对象通过正常 file-input change 提交。

### 叠加验证与命令

复用 N03 worktree 的临时 detached 树 `0b7cc752df197e9fb5ae950fdfbf481b0f859a46`，由 N13 HEAD `5f02b6818c1439323f397313f7078e7fc2004d81` 依次叠加 N15 代码提交 `e753cb3f2981cfe6d499672610ced054d509126e` 与兼容修复 `65db6ab27de69d96e4b1d54e61bfef3bfeaeff1f`。该树用于验证，与两份 PR 的提交范围分开。

- 修复后的真实 Redis 16 组合树中，旧创建路径新增元数据后，N13 关联文件列表从一份变为两份。临时记录随后回收。
- 组合 server 使用 N13 的隔离数据与 schema 16，可新建 22 字符 File Request 并保存 N13 的有限默认期限，单条列表读取正常。
- 独立 N15 与组合树分别构建 server，`go generate ./cmd/supashare` 与差异检查通过。组合树 Redis race 仅覆盖本次受影响的 N15 和 File Request 索引／配额用例。

```sh
go test -race -tags test,noaws ./internal/configuration/database/provider/redis -run '^TestN15' -count=1
go test -race -tags test,noaws ./internal/configuration/database/provider/redis -run 'TestN15|TestFileRequestRetentionAndIndexedFiles|TestFileRequestAtomicQuotaAndPage' -count=1
```

本地尚未覆盖全仓回归、其他浏览器／视口、真实 Redis 故障转移／断电、多应用进程混合负载、浏览器下载文件最终落盘、批量旧链接处置及生产部署。旧链接的既有风险由其原权限和有效期决定。N13 的全量 webserver 失败记录仍由对应 PR 保留，Actions 当前关闭。此次长度样本只用于核对生成路径和格式。
