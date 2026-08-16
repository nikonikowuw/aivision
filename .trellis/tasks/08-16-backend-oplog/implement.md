# 实施计划：操作日志中间件 + perm 权限码中间件 + 日志查询

## 步骤清单

1. **脱敏纯函数实现与测试**
   - 新建 `internal/pkg/mask/mask.go` 和 `internal/pkg/mask/mask_test.go`。
   - 验证递归 JSON 脱敏、敏感 key 大小写不敏感识别、4KB 截断。

2. **操作日志数据访问与业务层实现**
   - 在 `internal/repository/operation_log.go` 实现 `OperationLogRepository`（`Create`、`GetByID`、`ListPage`）。
   - 在 `internal/service/operation_log.go` 实现 `OperationLogService`。
   - 编写 `internal/service/operation_log_test.go`（使用 sqlite in-memory 验证日志记录与多维度条件查询）。

3. **操作日志中间件实现**
   - 扩展 `Identity` 支持 `Username`，确保认证通过后能够记录操作人账号。
   - 在 `internal/middleware/oplog.go` 实现操作日志拦截器，处理 Body 读取与重置、登录失败用户解析、敏感字段脱敏、goroutine 异步写库与 recover。

4. **Perm 权限码中间件实现**
   - 在 `internal/repository/menu.go` 或 `internal/repository/auth.go` 补齐权限码查询接口（或通过 `MenuRepository.GetPermissionsByRoleIDs`）。
   - 在 `internal/middleware/perm.go` 实现权限检查逻辑（super 直接放行，普通用户比对权限码，无权限返回 HTTP 403，未声明权限码写接口默认拒绝）。

5. **日志查询 API 与路由装配**
   - 在 `internal/api/operation_log.go` 实现 `OperationLogHandler`（`GET /api/oplog/page`、`GET /api/oplog/:id`）。
   - 为现有路由配置权限码声明（如 `system:menu:add`、`system:menu:edit`、`system:menu:delete` 等）。
   - 在 `cmd/api/wire.go` 装配新 Provider 并运行 `make wire`。
   - 更新 `router.go` 挂载 `OplogMiddleware`、`PermMiddleware` 和 `OperationLogHandler`。

6. **全链路验证与测试**
   - 编写 `internal/router/router_test.go` / `internal/middleware/oplog_test.go` / `internal/middleware/perm_test.go` 综合测试。
   - 运行 `make vet`、`make test`，确保全部通过。
