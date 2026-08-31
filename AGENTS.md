# AGENTS.md

## 项目事实（Project Facts）

### 概览

`Argus` 是一个边缘 AI 视频分析与 RBAC 管理系统，由三部分组成：

- `app/`：Go 后端管理服务（module `argus/app`）；
- `ui/`：vben-admin 5.7 Vue3 前端；
- `engine/`、`sdk/`、`algo-packages/`：C++20 媒体推理引擎（namespace `argus`）、共享 C ABI 契约 SDK 与独立算法插件包。

### 技术栈

- 后端 `app/`：Go 1.26、Gin、GORM（纯 SQLite 嵌入式驱动，WAL 模式）、google/wire（DI）、viper（配置）、zap（日志）、bcrypt（密码）。
- 前端 `ui/`：Vue 3 + Vite + TypeScript、ant-design-vue、Pinia、pnpm 11 + turbo monorepo、vben-admin 5.7.0。
- 引擎 `engine/` / `sdk/` / `algo-packages/`：C++20、CMake ≥ 3.24、gRPC/Protobuf、ZLMediaKit、VideoToolbox/Core ML (macOS)、纯 C ABI、nlohmann-json、GoogleTest。

### 常用命令

C++ 引擎与算法包（在 `engine/` 或 `algo-packages/` 下）：

- 引擎配置与构建：`make -C engine configure` / `make -C engine build`
- 引擎单测与契约测试：`make -C engine test`
- 引擎 ASan 内存安全测试：`make -C engine asan`
- 引擎代码风格与符号纯洁性检查：`make -C engine lint`
- 算法包单机编译与调试：`make -C algo-packages/macos/arm64/yolo26n build` / `make -C algo-packages/macos/arm64/yolo26n run`
- 算法包基准压测与分段耗时分析：`make -C algo-packages/macos/arm64/yolo26n benchmark`
- 算法包打包：`make -C algo-packages/macos/arm64/yolo26n package`
- 契约与 SDK 一致性检查：`bash algo-packages/scripts/sync-sdk.sh` / `bash algo-packages/scripts/check-consistency.sh`

后端（在 `app/` 下）：

- 迁移：`make migrate-up`（`go run ./cmd/migrate up`）/ `make migrate-version`
- 初始化管理员：`APP_BOOTSTRAP_ADMIN_PASSWORD="<pass>" make bootstrap-admin`
- 开发：`make dev`（自动执行 `migrate-up` 后启动 air）
- 构建：`make build`（输出 `bin/api`、`bin/migrate`、`bin/bootstrap`）
- 测试：`make test`（`go test ./...`）
- 检查：`make vet`（`go vet ./...`）
- 重新生成 DI 代码：`make wire`（改动 `cmd/api/wire.go` 后必须重新生成）

前端（在 `ui/` 下）：

- 安装：`pnpm install`（仅允许 pnpm）
- 开发：`pnpm dev`（单应用用 `pnpm dev:antd`）
- 构建：`pnpm build`
- 检查：`pnpm check`（circular + dep + typecheck + cspell）
- 测试：`pnpm test:unit`（vitest）
- 提交：`pnpm commit`（czg）

### 仓库结构

- `app/`：Go 后端。`cmd/api/` 启动入口与 wire 装配；`internal/model/` 8 张表 gorm 模型 + seed；`internal/pkg/` 下 config/logger/response/errno/db；`internal/router/` gin engine 装配点；`configs/config.yaml` 配置。
- `ui/`：前端 monorepo。`apps/web-antd/` 唯一业务应用（其 `src/api`、`src/store`、`src/router` 为自定义层）；`packages/*` 与 `internal/*` 为 vben 基础设施（`@vben/*`）。
- `.trellis/`：Trellis 工作流（spec、tasks、workspace）。
- `.agents/`、`.claude/`、`.codex/`：各平台 agent 配置、hooks 与技能。

### 架构事实

- 后端启动链：`config.Load()` → zap logger → SQLite GORM 连接（WAL 模式，自动建表与幂等 Seed）→ wire 装配 gin engine → 监听 `:8000`，SIGINT/SIGTERM 优雅退出（10s 超时）。
- 统一响应体 `{code,data,message}`：`code=0` 成功；业务错误码集中定义在 `internal/pkg/errno`（1xxx 业务码 + 401/403）。响应格式对齐前端 `defaultResponseInterceptor`（codeField/dataField/successCode=0）。
- 配置：`app/configs/config.yaml` + `APP_*` 环境变量覆盖（如 `APP_DB_PATH`、`APP_JWT_SECRET`）；纯 SQLite 嵌入式存储。
- 数据层约定：表名/列名显式声明（snake_case），不建外键，`status` 用 `int8`；`menus.title` 存 i18n key（决策 17）；`menus.type` 为 `catalog|menu|button` 字符串枚举。生产与开发环境 schema 变更统一走版本化 SQL 脚本（`app/migrations/`），AutoMigrate 仅供 sqlite 单元测试使用。
- 前端 API：`apps/web-antd/src/api/request.ts` 统一 `RequestClient`（Bearer token、自动刷新、code=0 成功）；已定义 `/auth/*`、`/user/info`、`/menu/all`。
- 开发代理：vite 把 `/api` 代理到 `http://localhost:5320/api`（当前为 mock，`apps/web-antd/vite.config.ts`）；后端 Go 服务在 `:8000`。

### 规范索引（编码前必读）

- `.trellis/spec/engine/index.md`：C++ 媒体推理引擎与算法包规范入口
- `.trellis/spec/backend/index.md`：Go 后端编码规范入口
- `.trellis/spec/frontend/index.md`：Vue3 前端编码规范入口
- `.trellis/spec/guides/index.md`：跨层思考与代码复用指南

> 前端的 oxlint/oxfmt/eslint/stylelint/typecheck/commitlint 已由 lefthook 在 pre-commit 强制，无需在规范中重复细则。

<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:

- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->
