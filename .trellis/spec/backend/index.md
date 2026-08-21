# 后端开发规范

> 本项目后端开发的最佳实践（Go / Gin / GORM / wire）。

---

## 概览

`app/` 下 Go 后端的规范。技术栈为 Gin + GORM（mysql/postgres），由 google/wire
装配；业务路由由功能任务按增量方式逐步添加。项目事实（命令、目录职责、架构）见
`AGENTS.md`；本目录文件描述**如何编写后端代码**。

---

## 规范索引

| 规范 | 说明 | 状态 |
| [目录结构](./directory-structure.md) | 模块组织与文件布局 | 生效 |
| [数据库规范](./database-guidelines.md) | ORM 模式、迁移、命名 | 生效 |
| [错误处理](./error-handling.md) | errno 错误码、响应契约 | 生效 |
| [日志规范](./logging-guidelines.md) | 结构化 zap 日志、日志级别 | 生效 |
| [质量规范](./quality-guidelines.md) | 禁止/必用模式、测试 | 生效 |
| [文件存储规范](./file-storage-guidelines.md) | 上传接口、存储抽象、local/MinIO 契约 | 生效 |

---

## 开发前检查清单

编写后端代码前阅读以下内容：

- [ ] **目录结构** — 新代码放在哪里（`internal/model`、需要时新增的
      `internal/service|repository|api|middleware`、`internal/router` 中的路由）：
      [directory-structure.md](./directory-structure.md)
- [ ] **数据库约定** — 显式 `TableName()`/列标签、不建外键、`int8` 状态、
      `BaseModel` 与 `TimeFields`、AutoMigrate（仅 dev/test）与版本化 SQL 迁移（生产）、
      幂等 seed：[database-guidelines.md](./database-guidelines.md)
- [ ] **错误处理** — 将错误码加入 `errno`；HTTP 错误统一由 gin 中间件输出，
      handler 只交错误/返回数据，绝不泄露内部细节：[error-handling.md](./error-handling.md)
- [ ] **质量关卡** — `make vet` + `make test` 全部通过、DI 变更后重新生成 wire、
      测试使用 sqlite 内存库：[quality-guidelines.md](./quality-guidelines.md)
- [ ] **文件存储** — 上传接口、存储抽象、文件校验和 local/MinIO 配置遵循：
      [file-storage-guidelines.md](./file-storage-guidelines.md)

另请阅读共享思考指南：`../guides/index.md`。

---

## 质量检查

完成后端工作前：

- [ ] `make vet` 干净，`make test` 全部通过。
- [ ] 无新增外键 / 驱动特定 gorm 类型 / `errno` 之外的错误码。
- [ ] 若 `wire.go` 或构造函数有改动，重新生成 `wire_gen.go`（`make wire`）。
- [ ] 代码、配置默认值或日志中无密钥。
- [ ] 新增配置键同时加入 `defaults()` 与 `validate()`。

---

**语言**：所有文档应使用**中文**编写。
