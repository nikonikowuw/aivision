# Go 工程骨架 + 8 表 model + AutoMigrate + seed + wire

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

搭好 `app/` Go 工程：可编译、可连接 MySQL、启动时自动建表并播种种子数据，wire 依赖注入跑通。本任务结束后 `go run ./cmd/api` 能起来，8 张表就位，admin 账号可查询。

## 依赖

无（序列第一个任务）。

## Requirements

- 工程骨架：`app/go.mod`（module `niko-vue-admin/app`）、`cmd/api/main.go`、`configs/config.yaml`、`Makefile`（wire/dev/build/test）。
- `internal/pkg/`：config（viper，环境变量 `APP_*` 覆盖，含 `db.driver` mysql/postgres 可选、默认 mysql）、logger（zap）、response（`{code,data,message}`）、errno（错误码表）、db（gorm 双驱动连接）。
- `internal/model/`：8 张表 gorm 模型——users、roles、menus、departments、user_roles、role_menus、refresh_tokens、operation_logs（结构见父 design.md §2）。menus 含 `title` 列（i18n key，决策 17）。
- 启动流程：viper 读配置 → gorm 连 MySQL → AutoMigrate → seed → wire 装配。
- seed 数据：admin/admin123（bcrypt）、super 角色（code=super）、默认菜单树（首页 dashboard + 系统管理 → 用户/角色/菜单/部门/操作日志，含按钮级权限码，catalog/menu 带 i18n key 的 `title`）、demo 部门。
- wire：`wire.go` ProviderSet 声明 + `wire_gen.go` 生成，`make wire` 可一键生成。

## Acceptance Criteria

- [ ] `go build ./...` 通过；`go vet ./...` 无错误。
- [ ] 配好数据库（`db.driver` 为 mysql 或 postgres）后 `go run ./cmd/api` 启动无 panic，日志显示 AutoMigrate + seed 完成；**两种驱动各验证一次**。
- [ ] 数据库 8 张表存在且结构正确；users 表有 admin（密码为 bcrypt 哈希）；menus 表菜单树完整（含按钮级），catalog/menu 的 `title` 为 i18n key（`routes.*`，与 design.md §5 title 契约一致）。
- [ ] `make wire` 重新生成 wire_gen.go 无 diff（或生成成功可编译）。
- [ ] config.yaml 参数齐全（server/db/driver/jwt/log）；`db.driver` 非法值配置加载报错。

## Out of Scope

- 所有 HTTP 接口与中间件（后续子任务）
- 任何业务逻辑（登录、CRUD）
