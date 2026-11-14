# N05：API Key 并发编辑不再恢复已撤权限

## 范围与基线

对应 2026-11-14 CSV 第 15 行，finding `acf_058d62fa5108b2437b8f194a30f25473`。
基线 `fb8c9f3a201315a043f72f7cfdf05d7d86d7ea7c`，即 PR #104 合并后的 master。
本组不修改认证协议、数据库结构、密钥格式或版本号。

## 修复

- 公共 ID 在加锁前解析为私有 ID。编辑、重命名与删除使用统一入口，先锁相关用户，再以 `LockMany` 锁调用凭据及目标凭据，去重并避免自编辑/条带碰撞死锁。
- 锁内重新读取调用凭据、当前用户、目标及其所有者，重新检查 ApiMod 与目标管理权限。入口处已授权不代表等待结束后仍有权限。
- 名称修改仅写 `FriendlyName`。权限修改基于数据库当前值原子增加或清除指定权限位，而不是回写旧权限快照。
- 用户降权时逐个密钥原子清除对应权限位，不再依据旧快照跳过清除，不覆盖名称或其他位。
- SQLite 使用单条 UPDATE，Redis 使用单个 Lua 脚本。两者均只更新现存记录，不会因并发删除而重新创建凭据；保留所有者、公有 ID、分类、使用时间和过期信息。

## 保证边界

字段级原子修改对共享同一 SQLite 数据库或 Redis 的独立实例有效。进程内锁同时保护调用方和目标的管理操作；它不是分布式锁。
**跨实例用户/调用凭据授权检查与写入并非跨实体原子事务**，本组不宣称全局撤权线性化。已通过检查且与另一实例撤权重叠的请求仍可能完成其明确请求的变更，但不会通过旧对象回写恢复无关权限。
同一权限位的显式授予和撤销按数据库实际执行顺序生效；本组不将合法后发授予永久禁止。

字段更新不延长有效期。Redis 到期后实体不存在，更新返回未找到；SQLite 过期记录在清理前可能仍能更新字段，但其 Expiry 不变，API 认证仍拒绝过期凭据。升级应停止旧实例，避免旧版本整对象更新重新引入丢失更新。

## 验证

使用 Go 1.26.10：

```sh
go test -tags test ./internal/webserver/api ./internal/configuration/database/...
go test -race -tags test ./internal/webserver/api ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -run TestN05 -count=1
```

两项通过，另有独立复审与三包 race 复跑。

新增覆盖：公共/私有 ID 并发操作、独立 provider 实例并发改名/授予/撤销、无关字段保持、删除不复活、Redis TTL 保持、调用凭据已撤权后拒绝、自己修改自己的凭据、等待锁期间所有者权限变更后拒绝授予。
Redis 回归使用 miniredis，未在本轮声称真实 Redis 服务验证；未进行浏览器或生产部署测试。

补充：SQLite 过期行不改 Expiry、Redis TTL 到期后更新不复活的专项已通过。
全仓测试曾因尚未生成 WASM 无法编译；随后 `go generate ./...` 成功。再次尝试全仓 `-tags test,noaws` 时未获得完整结果，已停止等待，不能报告全仓通过。本组依据最终代码的 API/数据库全量及专项 race 结果交付；真实 Redis、完整仓库集成测试留给本地复核。
