# 操作日志中间件 + 日志查询

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现操作日志全自动采集中间件 + perm 权限码中间件 + 日志查询接口。本任务结束后所有写操作自动入日志表（脱敏），接口权限码开始强制执行。

## 依赖

- `08-16-backend-auth`（中间件链位置）。
- `08-16-backend-user/role/menu/dept`（存在可被记录的写接口；perm 中间件生效需 router 已声明权限码）。

## Requirements

- R-5.1 中间件拦截 POST/PUT/DELETE（登录/登出也记录，module=auth），body 读取后重置请求体。
- R-5.2 记录字段见父 design.md §2 operation_logs；password/oldPassword/newPassword/token 类字段脱敏为 `***`，body 截断 ≤4KB。
- R-5.3 异步 goroutine 落库（带 recover），写失败仅 zap warn，不影响业务响应。
- R-2.4 perm 中间件：按路由声明权限码比对用户权限集合（`*` 直接放行），不满足返回 HTTP 403；未声明权限码的写接口默认拒绝。
- R-3.5 日志查询：`GET /oplog/page`（分页，筛选：时间范围/用户名/模块/状态码）、`GET /oplog/:id`（详情）；只读。

## Acceptance Criteria

- [ ] 父 AC-11：任一写操作后日志列表出现记录（含 username/module/path/status_code/耗时），密码字段为 `***`；GET 请求不产生日志。
- [ ] 登录失败也产生日志（user_id 空、username 为请求体中的用户名）。
- [ ] 无权限码用户调写接口返回 HTTP 403；super 全放行。
- [ ] 日志查询按时间/用户名/模块/状态码筛选正确。
- [ ] 日志写入失败不影响业务接口响应。
- [ ] `go test ./...` 通过（含脱敏纯函数用例）。

## Out of Scope

- 日志自动清理、清空接口、前端日志页面（frontend-pages）
