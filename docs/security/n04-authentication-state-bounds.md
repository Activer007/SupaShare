# N04：临时 API 凭据与匿名认证状态限额

## 范围与基线

基于 master `9b5d8bc2dd4317e36c9ede7f2f3bad8e39d3cc8c`（PR #103 合并）。
仅处理 2026-11-14 新安全 CSV 的两项：

- 第 14 行 `acf_212f4f4648e314ba6a73792a3ecd900d`：认证会话可持续写入临时 API 凭据。
- 第 21 行 `acf_b727da9425f8e9eac24664739ab208e9`：匿名登录初始化耗尽 CSRF/OIDC 状态。

不包含 N05 的 API key 并发编辑、不改密码认证代次、会话 Cookie 或 OIDC
PKCE/nonce、回调绑定规则。本批无 schema 迁移，不提升版本、不打标签。

## 实现与用户可见变化

### 临时 API token

- `/auth/token` 仍在通过原有认证和权限校验后签发 5 分钟凭据。
- 每用户最多 64 个、全库最多 4096 个尚未清理的系统临时凭据。普通 API key
  和文件请求能力不占此预算，也不被此路径清理。
- 采用限额而非轮换/复用：避免另一个标签页刷新 token 时撤销正在使用的凭据，
  也不引入会话与 token 绑定的迁移。到期前的原有 token 不延长、不淘汰。
- SQLite 先在写事务内清理至多 256 个 `Expiry <= now` 的过期系统凭据，
  再在同一事务内检查用户/全局数量并插入。拒绝仍提交清理；遗留大量过期行
  可能需多次请求或原有后台清理后恢复。剩余过期行保守计入预算。
- Redis 用单个 Lua 脚本扫描、清理、校验、插入及设置 TTL，不用易漂移计数器。
  最多 128 次 SCAN、20000 个返回记录，重复记录去重；超扫描预算拒绝写入。
  即使配置前缀含 glob 字符也只统计本命名空间。显式过期清理至多 256 个，
  Redis 正常 TTL 过期独立生效。
- SQLite 双连接、Redis 多客户端共享同一数据库时限额原子成立；不是仅进程锁。
  新旧版本混跑时旧实例仍可绕过新准入，因此应统一升级。
- 满额或扫描预算不足返回 `429` 和 `Retry-After: 60`，不返回凭据。
  临时 token 满额提示等待过期，而不是要求用户删除不可管理的系统凭据。
  60 秒是建议重试间隔，不保证该时刻一定恢复；正常 token 最长五分钟过期。

### 匿名认证状态

- 登录、退出、改密和 OIDC 四种用途分别限制：每可信来源 IP 每分钟 120 次，
  每用途全局每分钟 600 次；采用非阻塞准入，不堆积等待请求。
- 限流器自身每用途最多保留 600 个有效来源槽位，过期回收，拒绝不占新槽位。
- CSRF 保留浏览器绑定和一次消费：每用途最多 4096 个、同绑定最多 16 个、
  同 IP 最多 64 个。插入前同锁清过期，再校验配额；每分钟后台清理。
  登录不能吃掉退出/改密池；满额不淘汰其他浏览器有效 token。
- OIDC 保留全局 4096 个、十分钟 TTL，新增每 IP 最多 64 个；同锁清理和准入。
  IP 只用于发起配额，回调仍验证浏览器绑定，移动网络切换不会仅因 IP 变化被拒绝。
  不改变旧标签页回调不得清掉新标签页 Cookie 的行为。
- `/login`、`/changePassword`、`/logoutToken`、OIDC 发起流程遇限额明确返回
  `429`、`Retry-After: 60`、`Cache-Control: no-store`，不写替换 Cookie、
  不输出空 token 的成功表单，也不重定向到 IdP。
- 退出 token 不要求会话仍可认证，继续允许轮换前驱撤销其后继会话。

## 部署和剩余边界

匿名状态及限流为进程内。多实例需粘性路由，重启或跨节点回调继续 fail closed
并要求重新登录；本批没有引入共享 CSRF/OIDC 数据库，不声称全站共享匿名限额。
总内存有界不等于抵御任意分布式洪泛；应在可信反代/入口另设全站速率与连接限制。

来源 IP 复用现有可信代理配置与 `logging.GetIpAddress`，不直接相信任意
`X-Forwarded-For`。应正确配置可信代理；否则代理后的用户会共享代理 IP 预算。
NAT 下多个用户本来就共享 IP 配额，64 个同时未完成流程可能被共同占满。
完成/消费、到期后容量恢复；本批不增加动态配置页面或复杂配额管理。

Redis 混合大键空间超过扫描预算时会保守拒绝临时凭据签发，已有凭据及删除不受影响。
管理员应检查相关命名空间和历史数据，而不是关闭限制。应用与 Redis 主机需要时钟同步。
回滚会失去新增限额，应保留入口流量限制；无需逆向数据库迁移。

## 验证（2026-11-14，云端）

Go 1.26.10；`GOPATH`、`GOCACHE` 使用工作区缓存。
当前 worktree 的 VCS stamping 不可用，生成/构建使用 `GOFLAGS=-buildvcs=false`。

- `go generate ./...` 通过，无新增受跟踪生成物差异。
- `go test -race -tags=test,noaws ./internal/configuration/database/... -run TestN04 -count=3` 通过。
  SQLite/miniredis 各双连接验证用户/全局边界、并发仅剩名额、删除后恢复、
  碰撞不覆盖、普通/文件请求 key 隔离；SQLite 验证256条分批清理及精确到期。
  另以生产 Lua 脚本模拟 SCAN 空页/重复页，验证有界拒绝。
- `go test -race -tags=test,noaws ./internal/webserver/authentication/... -count=1` 通过。
  含临时 token 实际 Generate、CSRF 配额与消费、OIDC 总容量、并发与公平，
  以及原有 HTTP/TLS 反代模拟、Cookie、密码重置和 session lineage 回归。
- `go test -race -tags=test,noaws ./internal/webserver -run 'TestN04|Test.*[Ll]ogout|Test.*[Cc][Ss][Rr][Ff]|Test.*[Tt]oken' -count=1` 通过。
  满额的登录/改密/退出/token handler 均返回明确429，旧Cookie和旧token保持有效。
- Web 全套唯一失败为 `TestAdminRenewalAuth: Session not renewed`，已在未修改的
  精确 `9b5d8bc` 基线独立复现；不将该既有失败标记通过。
- `go build -tags=noaws ./cmd/supashare` 通过。
- 独立审查认证相关代码及认证三个子包 race 通过。

真实 Redis 服务、多节点部署、生产反代/NAT 压测与真实 IdP 未验证。
miniredis 与本机 HTTP/TLS 模拟不能替代生产验收。
GitHub Actions 仍按仓库设置关闭；本批尚未创建 PR、合并或部署。
