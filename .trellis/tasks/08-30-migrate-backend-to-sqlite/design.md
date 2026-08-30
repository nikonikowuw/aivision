# Design — Go 后端全面迁移为纯 SQLite (WAL 模式)

## 1. 架构与设计决策

### 1.1 架构演进
- **原架构**：PostgreSQL 独立数据库 + golang-migrate 22 个 SQL 迁移版本 + GORM (postgres driver) + 连接重试。
- **目标架构**：嵌入式 SQLite 数据库（`data/argus.db`）+ WAL 模式（Write-Ahead Logging）+ 并发忙等待超时控制（5000ms）+ GORM `AutoMigrate` 自动维护 Schema + 内置 `Seed` 初始化。

### 1.2 关键设计决策
1. **纯单机驱动 (SQLite Only)**：
   - 彻底移除 `gorm.io/driver/postgres` 及 `golang-migrate`，消除抽象泄漏和双驱动维护成本。
   - 使用成熟且广泛支持的 `gorm.io/driver/sqlite`（即 `mattn/go-sqlite3`），并优化 DSN 连接参数。
2. **DSN 与并发控制**：
   - DSN 参数：`file:<path>?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache=shared&_foreign_keys=ON`
   - 连接池配置：
     - 在 WAL 模式下，SQLite 支持多并发读与单写并发排队（通过 busy_timeout 自动重试）。
     - 设置合理的 `MaxOpenConns`（推荐 10-20），`MaxIdleConns`（推荐 5-10），避免过度占用文件句柄，同时保证读并发吞吐。
3. **数据目录与自动建库**：
   - 连接数据库时自动检测并创建数据库文件所在目录（如 `data/`）。
4. **Schema 管理与初始化**：
   - 启动流程（`cmd/api/main.go`、`cmd/bootstrap/main.go`）简化为：
     1. 连接 SQLite（`db.New(cfg, log)`）
     2. 自动迁移表结构（`model.AutoMigrate(gdb)`）
     3. 幂等播种初始化数据（`model.Seed(gdb)`，包含系统角色、菜单权限、初始部门等）
   - `cmd/migrate` 工具可简化为状态检测/迁移工具，或执行 `AutoMigrate`。
5. **SQL 方言统一**：
   - `internal/repository/task.go` 中的 `GetTaskStats` 查询将 `COUNT(*) FILTER (...)` 替换为 `SUM(CASE WHEN ... THEN 1 ELSE 0 END)`，确保跨 DB 语义通用。
   - `BumpRevisionTx` 原生的 `UPDATE ... RETURNING revision` 在现代 SQLite (3.35+) 中受原生支持，保留且增加单测覆盖。

## 2. 配置模型调整

`configs/config.yaml` 简化为：
```yaml
db:
  path: "data/argus.db"
  busy_timeout: 5000 # 毫秒
  max_open: 20
  max_idle: 5
  max_lifetime: "1h"
```

环境变量覆盖支持 `APP_DB_PATH` 等。

## 3. 风险与回退
- **文件锁与高并发**：
  - 风险：瞬时超高频写导致 `busy_timeout` 超时。
  - 缓解：系统作为边缘视频分析管理后台，配置写操作频率低，告警记录写入有批处理缓冲，5000ms busy_timeout 足以保证无锁竞争失败。
