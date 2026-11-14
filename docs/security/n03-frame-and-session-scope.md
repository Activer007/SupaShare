# N03：管理页面防嵌入与会话 Cookie 作用域

## 范围与基线
基于 master `42042b093b52d162903dd3f2b652a4c49670a76b`（PR #102）。
只处理新 CSV 第 20、22 行：
- acf_6b285afe6cddcc411bdb2406c0fcfb7e：同站兄弟域嵌入管理页。
- acf_2eb8af630f505b43d8d0bad9344f9e53：同名/路径 Cookie 遮蔽及退出残留。
不包括 N04、API token 生命周期、OIDC 资源限额或数据库结构变更。

## 行为
- 所有非公开应用模板默认发送强制 CSP `frame-ancestors 'none'` 和
  `X-Frame-Options: DENY`，包含登录、忘记密码、修改密码、管理页、错误页、
  索引页；独立安装向导也加同样限制。原 CSP report-only 保留。
- 公开下载、密码下载、文件名跳转、公开上传和交付页保持原嵌入策略。
  StreamSaver 静态 iframe 不注入应用页 CSP。
- 根据已配置的外部 ServerUrl 协议决定 Cookie，不信任请求转发头。
  HTTPS 使用 `__Host-session_token; Secure; Path=/` 且不设置 Domain。
  HTTP 保留 `session_token`，明确 `Path=/`，兼容本机开发。
- 登录、轮换、退出使用同一作用域。退出设置过去的 Expires 和 Max-Age=-1。
- 认证和退出只接受恰好一个非空 canonical Cookie；即使两个值完全相同也拒绝。
  HTTPS 下遗留普通名称 Cookie 不能认证，也不能覆盖 canonical Cookie。
- 写入 canonical Cookie 时清除旧 host-only `/auth` Cookie；
  HTTPS 同时清除旧 host-only 根 Cookie。收到遗留 Cookie 时，
  HTTPS 或 HTTP 重名请求还会清理已知历史 /auth 路径。
  请求 Cookie 不包含 Domain/Path，不能声称自动清除所有任意路径和父域 Cookie。
- 退出仍使用既有持久化 session lineage 撤销 successor，不改变 N02 认证代次校验。

## 升级与限制
HTTPS 的旧 Cookie 不会自动换成新 Cookie，用户须重新登录。这是有意的安全边界，
否则可能将兄弟域投放的 legacy Cookie 转正。数据库不迁移、不整体清会话。
新旧服务器混跑会使用不同 Cookie 名称，故应统一升级实例；回滚会改变 Cookie 协议，
应先停止新实例并清除浏览器本域会话 Cookie，再登录。

HTTP 环境无浏览器 `__Host-` 安全保证，仅用于可信本机/开发环境；
公网必须配置 HTTPS。若 HTTP 的父域 Cookie 导致持续重名，必须在浏览器清除对应
站点/父域 Cookie 后重登；服务端拒绝选择“第一个”或“看起来有效的”Cookie，
也不猜测父域并清除其他应用 Cookie。

## 验证
- 认证全部子包通过，包括 HTTP 与 TLS 反代 OIDC 登录/退出模拟。
- setup 套件通过；独立安装向导实际 handler 的 HTML 与防嵌入响应头通过。
- N03 专项及 SEC021 session lineage race 三次通过。
- Cookiejar 验证历史 /auth 与 HTTPS legacy 根 Cookie 清理，
  /auth/token 轮换后根路径、新旧名称并存、多个 Cookie header 与同值重名拒绝，
  predecessor 退出撤销 successor、重名退出不任意撤销会话。
- go generate（GOFLAGS=-buildvcs=false）、noaws 程序构建及 git diff --check 通过。
  本云端 worktree 的 VCS stamping 不可用，关闭 stamping 仅用于生成/验证。
- Web 防护策略、登录、改密、退出专项最终通过。Web 全套唯一失败为
  TestAdminRenewalAuth（Session not renewed），已在未修改的 42042b0 基线单独复现。
  不将该已知失败计为通过；本批未修改它。
- 未做真实浏览器跨站 iframe 攻击演练、生产反代升级和多实例混跑验证。
  HTTP 单机兼容不代表公网 HTTP 安全。无新的 PR、标签或部署操作。
