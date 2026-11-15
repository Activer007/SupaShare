# N17：日志高可用韧性防护与 SVG 热链安全沙箱防护

基线：`2752156`（PR #115 合并后）。聚焦解决全面安全审查中发现的高危缺陷：

1. **日志写操作未捕获 panic 导致的远程拒绝服务 (Remote DoS via Goroutine Panic)**
2. **SVG 热链过期图片模板注入与存储型 XSS (Stored XSS / XML Injection in SVG Hotlink)**

---

## 一、漏洞机理与威胁模型

### 1. 异步 Goroutine 未捕获 Panic 引发远程拒绝服务
- **位置**：`internal/logging/Logging.go`
- **机理**：系统在记录常规访问或文件下载日志时，通过独立 Goroutine `go writeToFile(output)` 异步写入日志。原实现使用 `helper.Check(err)` 处理 `os.OpenFile`、`WriteString` 及文件读取错误，发生错误时直接触发 `panic(err)`。
- **威胁场景**：当宿主发生磁盘空间占满、文件描述符耗尽或日志文件路径权限受限时，任何用户请求触发的异步写日志操作将直接导致 Go runtime 崩溃（`exit status 2`），造成全站拒绝服务（DoS）。

### 2. SVG 热链过期图片模板注入与 XSS
- **位置**：`internal/webserver/Webserver.go` (`loadExpiryImage`, `showHotlink`)
- **机理**：
  1. `loadExpiryImage` 使用 `text/template` 解析 `web/templates/expired_file_svg.tmpl`，未对 `configuration.Get().PublicName` 进行 HTML/XML 上下文转义。
  2. 当 `/h/{id}` 热链指向不存在或已过期的文件时，响应头设置为 `Content-Type: image/svg+xml`，但未配置 `Content-Security-Policy` 或 `X-Content-Type-Options`。
- **威胁场景**：若站点名称被配置或注入了包含 SVG 标签或 `<script>` 的恶意载荷，在浏览器直接访问该热链 SVG 时将执行任意 JavaScript 脚本，导致存储型 XSS 或 XML 注入攻击。

---

## 二、修复实现

### 1. 日志层异常恢复与错误降级 (`internal/logging/Logging.go`)
- 在 `writeToFile` 入口处添加 `defer func() { if r := recover(); r != nil { ... } }()`，兜底阻止任何未知 panic 向外扩散。
- 移除 `writeToFile`、`deleteAllLogs`、`DeleteLogs`、`GetAll`、`GetSince` 中的 `helper.Check(err)`，改为优雅的 `fmt.Fprintf(os.Stderr, ...)` 错误日志输出与安全降级。
- 互斥锁统一采用 `defer mutex.Unlock()` 模式，防止异常路径发生死锁。
- 新增单元测试 `TestWriteToFileResilience` 验证不可写路径下的高可用韧性。

### 2. SVG 模板上下文转义与沙箱隔离 (`internal/webserver/Webserver.go`)
- 切换 `loadExpiryImage` 的模板引擎为安全转义的 `html/template`，对动态插值进行严格的 XML 实体转义。
- 提取统一的 `writeExpiredSvg(w)` 处理逻辑，强制下发纵深防御响应头：
  - `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`
  - `X-Content-Type-Options: nosniff`
  开启 `sandbox` 模式彻底剥离脚本执行能力与会话凭证访问能力。
- 新增专项测试 `TestDownloadHotlinkExpiredSvgSecurity`，断言转义行为与安全响应头。
- 修复历史测试 `TestAdminRenewalAuth` 因会话期限单调性校验产生的基线冲突，确保全套 Web 服务测试绿灯。

---

## 三、验证记录

- `internal/logging` 测试套件：全部通过（含 `TestWriteToFileResilience`）。
- `internal/webserver` 测试套件：全部通过（75s，零回归）。
- `internal/webserver/authentication/...` 测试套件：全部通过。
