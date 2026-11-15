# N18：中危安全加固与传输作用域完整性防护

基线：`708d696`（PR #116 合并后）。聚焦解决全面安全审查中识别的中危与架构配置隐患：

1. **单文件下载密码验证 Cookie 作用域缺失 (`writeFilePwCookie` 遗漏 `Path: "/"`)**
2. **全站缺少 HSTS (HTTP Strict Transport Security) 响应头**
3. **反向代理 127.0.0.1 限流碰撞风险与未配置信任前置预警**
4. **SSE 长连接循环中 `time.After` 定时器累积泄漏与异常防护**
5. **Docker 容器生产非 root 部署加固指引**

---

## 一、漏洞机理与威胁场景

### 1. 单文件下载密码 Cookie 缺少 Path 作用域限定
- **位置**：`internal/webserver/Webserver.go` (`writeFilePwCookie`)
- **机理**：单文件密码通过验证后，系统写入 Cookie `p<fileId>` 作为已授权凭证。原实现未设置 `Path` 属性。根据 RFC 6265，未显式声明 Path 的 Cookie 将默认继承发起请求的 URI 目录。
- **威胁场景**：当部署存在反向代理路径重写、或通过不同端点（如 `/d?id=...` 与 `/downloadFile?id=...`）访问时，浏览器可能限制该 Cookie 的发送路径，导致用户需重复输入密码或下载失败；同时缺少显式根作用域声明不符合现代 Cookie 最小作用域与凭据规范。

### 2. 全站缺少 HSTS（HTTP 严格传输安全）头
- **位置**：`internal/webserver/ResponsePolicy.go` (`withResponsePolicy`)
- **机理**：项目虽然在 `withResponsePolicy` 中配置了 `X-Content-Type-Options: nosniff` 和 `Referrer-Policy: no-referrer`，但即使在配置了对外 HTTPS (`configuration.UsesHTTPS()`) 或开启原生 TLS 时，始终未下发 `Strict-Transport-Security`。
- **威胁场景**：首次通过 HTTP 访问的用户在发生重定向到 HTTPS 之前，或者处于公共网络中的用户，容易遭受 SSL 剥离（SSL Stripping）与中间人降级攻击（Man-in-the-Middle Attack）。

### 3. 反向代理信任缺失引发的全局限流封锁隐患
- **位置**：`internal/logging/Logging.go` (`GetIpAddress`)
- **机理**：当通过反向代理（如 Nginx、Traefik、Cloudflare Tunnel 或 Docker 内部容器网桥）转发流量且运维未显式配置 `TRUSTED_PROXIES` 时，`GetIpAddress` 无法信任任何代理标头（`X-Forwarded-For`、`CF-Connecting-IP`、`X-Real-IP`），只能退化采用直连 Peer IP。
- **威胁场景**：所有外部用户的请求在速率限制器（RateLimiter）中均归为反向代理本身的同一 IP。恶意访问者仅需故意尝试 3 次错误登录即可触发全局限流，导致正常用户同时被封禁，引发全站拒绝服务（DoS）。且此前由于缺乏诊断输出，管理员排查极为困难。

### 4. SSE 长连接 `time.After` 资源堆积与序列化 panic
- **位置**：`internal/webserver/sse/Sse.go` (`GetStatusSSE`)
- **机理**：
  1. 长连接心跳监听循环内部直接使用 `case <-time.After(pingInterval):`。根据 Go 运行时机制，`time.After` 创建的 `time.Timer` 在未超时前无法被垃圾回收器回收。在大量客户端并发或高频推送上传进度事件时，每次循环均会新增一个 15 秒存活的定时器，造成瞬时堆内存膨胀与 GC 停顿。
  2. 初始状态同步循环中直接调用 `helper.Check(merr)`，若发生序列化异常直接导致 Goroutine 抛出 panic。

---

## 二、修复实现与加固方案

### 1. 显式限定凭据 Cookie 作用域 (`internal/webserver/Webserver.go`)
- 在 `writeFilePwCookie` 中显式配置 `Path: "/"`，保证其在全站所有下载相关端点（`/d`、`/downloadFile`、`/dh/...`）的传输确定性与规范一致性。
- 在 `internal/webserver/security_cookie_test.go` 中补充 `c.Path == "/"` 的自动化断言。

### 2. 全站启用 HSTS 响应头 (`internal/webserver/ResponsePolicy.go`)
- 在统一响应策略中间件 `withResponsePolicy` 中注入条件检查：
  ```go
  if configuration.UsesHTTPS() {
      w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
  }
  ```
  在启用 HTTPS 时向所有经过服务的响应注入标准 2 年期包含子域的 HSTS 标头，防御 SSL 剥离；在非 HTTPS 环境下保持不发送。
- 在 `internal/webserver/security_response_policy_test.go` 中新增 `TestSEC050HSTSHeader` 专项测试。

### 3. 反向代理前置诊断安全告警 (`internal/logging/Logging.go`)
- 在 `GetIpAddress` 引入非可信代理转发标头前置检查：
  ```go
  var warnUntrustedProxyOnce sync.Once

  func checkUntrustedForwardingHeader(r *http.Request, peer string) {
      if len(r.Header.Values("X-Forwarded-For")) != 0 || len(r.Header.Values("CF-Connecting-IP")) != 0 || len(r.Header.Values("X-Real-IP")) != 0 {
          warnUntrustedProxyOnce.Do(func() {
              fmt.Fprintf(os.Stderr, "WARNING: Received proxy forwarding headers from untrusted peer %s. Client IP rate limiting will fall back to peer IP. If this is a reverse proxy, add it to TRUSTED_PROXIES.\n", peer)
          })
      }
  }
  ```
  在保留 ADR-003 严格安全信任边界（绝对不轻信未经授权的转发标头）的前提下，向运维控制台输出清晰诊断提示，防止运维疏漏造成单 IP 限流碰撞。
- 在 `internal/logging/Logging_test.go` 中新增 `TestCheckUntrustedForwardingHeader`。

### 4. SSE 资源回收与稳态优化 (`internal/webserver/sse/Sse.go`)
- 将 `for` 循环中的 `time.After(pingInterval)` 改造为外层单例 `ticker := time.NewTicker(pingInterval); defer ticker.Stop()` 和内部 `case <-ticker.C:`，消除循环内部重复分配定时器。
- 将 `helper.Check(merr)` 改为安全错误忽略与降级，保障长连接稳健性。

### 5. 容器生产非 root 部署说明 (`docker-compose.yaml`)
- 在默认 `docker-compose.yaml` 开头追加明确注释，引导生产环境采用基于 UID/GID 10001:10001、只读根文件系统与 Capabilities 剥离的 `docker-compose.nonroot.yaml`（遵循 ADR-005 规范）。

---

## 三、验证结果

- `go test -v -tags=test,noaws ./internal/webserver`：**PASS**（全量用例通过，包含 `TestSEC012FileCookieTransport` 与 `TestSEC050HSTSHeader`）
- `go test -v -tags=test,noaws ./internal/logging`：**PASS**（全量用例通过，包含 `TestCheckUntrustedForwardingHeader`）
- `go test -v -tags=test,noaws ./internal/webserver/sse`：**PASS**（全量用例通过）
