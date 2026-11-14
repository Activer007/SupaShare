# N02：密码重置与旧密码登录竞态

范围：新安全 CSV 第 3 行 `acf_258de629af5aa4093f29475dfa174040`。基线为合并 PR #101 后的 `fe9a572`。不扩展到 N03 或其他修复组。

## 安全约束

- 用户与浏览器会话持久化认证代次（AuthGeneration）。管理员重置、自助强制改密、部署更新密码原子递增代次，包含密码值没有改变的重置。
- 密码验证得到的原始用户快照传递给会话签发。SQLite 条件 INSERT / Redis Lua 原子核对用户存在、密码哈希、ResetPassword 和代次后才保存会话；失败不发 Cookie。
- 每次会话使用检查当前用户代次。轮换继承原代次，并用原子签发检查，不能把旧会话提升为新代次。重置后的安全性不依赖删除会话的扫描时序。
- 旧格式密码升级仅对已验证的哈希、重置状态和代次执行 CAS，且只更新哈希。并发重置或删除后升级失败，不能恢复旧密码或用户。
- 自助改密用已认证请求的用户快照执行 CAS。重置前已进入处理器的旧会话无法在重置完成后改写新密码。
- 用户资料、权限、等级修改改为窄字段 CAS；冲突返回 HTTP 409，要求重新加载。不会回写凭据，也不会在用户删除后重新创建。只有成功更新后才修改 API Key 权限。
- `SaveUser` 保留可信创建、离线导入和测试夹具用途。应用现有用户的凭据和资料更新必须使用相应 CAS 方法，不得用通用 upsert。

## 发布与兼容

SQLite schema 22；Redis schema 14。升级保留用户哈希并清除旧浏览器会话，所有用户需要重新登录。SQLite 新列初始化可重试；离线读取旧 SQLite 用户表兼容缺少代次的历史 schema。OIDC 新用户默认代次 0，签发和轮换同样绑定代次。

必须停止所有旧实例、备份数据，然后统一升级再启动。**不支持新旧版本混跑，也不能把已升级数据库交给旧程序。** 恢复旧版必须恢复相匹配的数据库备份，且清除浏览器会话。

本改动保证重置与会话签发的数据库原子检查，不宣称现有会话退出/继承链协议跨多个服务实例完全线性化。现有 lineage 操作仍有进程锁；这是独立范围，不应在本组中夸大验收。

## 回归范围

- SQLite / Redis 两个独立连接，通过屏障固定「旧密码验证完成 → 重置完成 → 延后签发」，确认拒绝旧请求。
- 已签发会话在不清理会话表的情况下失效；旧会话不可轮换；新的凭据快照可以签发。
- 相同哈希重置也换代；旧 ResetPassword 会话并发自助改密失败。
- 旧哈希升级与重置、删除竞争；资料更新不覆盖哈希、不恢复已删除用户、不静默宣告成功。
- 数据库升级、重复升级、旧库只读迁移、OIDC 与已有会话 lineage 回归。

Redis 自动化使用 miniredis 执行 Lua，不等同于真实 Redis 部署验收。提交前应在实际 Redis 版本执行发布回归。浏览器端真人操作未在本组云端自动化中开展。

### 云端验证记录（2026-11-14）

- Go 1.26.10；`go test -tags test ./internal/webserver/authentication/... ./internal/configuration/database/... ./internal/webserver/api -count=1` 全部通过。
- `go test -race -tags test ./internal/webserver/authentication/sessionmanager -run 'TestN02|TestSEC021' -count=3` 通过。
- 两 provider 的 AuthGeneration / ResetCredentials / Profile 专项 `-race -count=3` 通过。
- `go test -tags test ./internal/webserver -run 'Test.*Password|Test.*Login|Test.*Logout|Test.*Reset' -timeout 90s` 通过。
- 完整 webserver 套件单独运行耗时 91.269 秒，唯一失败为 `TestAdminRenewalAuth: Session not renewed`。在未修改的 `fe9a572` 基线独立运行该测试，同样失败（1.038 秒），确认是已有问题，未纳入 N02。早期并行运行共享测试目录出现的干扰不作为验收证据。不得把专项通过写成全仓库测试通过。
- 独立代码复核关闭了旧资料回写密码、删除后复活、权限撤销静默失败三项集成问题。
