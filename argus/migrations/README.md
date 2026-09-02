# 数据库迁移说明（SQLite）

当前 `app/` 的运行时数据库是 SQLite，数据库初始化和升级由 GORM 负责：

```text
cmd/migrate up/version
        -> internal/pkg/migration.Runner
        -> model.AutoMigrate
        -> model.Seed
```

API 启动时也会执行同一套 `CheckSchemaReady`。因此 SQLite 数据库不存在独立的 migration version 表，`make migrate-up` 不会读取或执行 `migrations/*.sql`，也不支持 `down` 和 `force` 命令。

## 常用命令

在 `app/` 目录下执行：

```bash
# 自动建表/补列并播种幂等初始数据
go run ./cmd/migrate up
# 检查并补齐 schema 与 seed
go run ./cmd/migrate version
```

对应的 Make 目标为 `make migrate-up` 和 `make migrate-version`。

## 已有 SQLite 数据库升级

直接再次执行 `make migrate-up` 即可。GORM `AutoMigrate` 会补齐新增表、字段和索引，`Seed` 会增量补齐菜单，并清理已经废弃的 `RecordPlate` 菜单及其角色关联。SQLite 生产数据不应通过删除数据库文件或手工重建表升级。

## SQL 迁移文件

`migrations/*.sql` 是按版本保存的 PostgreSQL 发布脚本，使用 `embed.FS` 保存在 Go 代码中，供未来或独立的 PostgreSQL 发布工具使用；它们不是当前 SQLite CLI 的执行来源。修改已经发布的 SQL 文件不能替代前向迁移：例如 `000040_add_capture_joint_fields` 用于补齐已由 `000038` 创建的 `captures` 表字段，`000041_remove_legacy_record_plate_menu` 用于清理旧菜单。

PostgreSQL 部署必须使用支持这些脚本的外部 migration runner，并在 API 启动前完成全部待执行版本。SQLite 与 PostgreSQL 的 schema 变更应保持模型和脚本语义一致，并分别在对应运行时验证。

## 生产注意事项

- 默认管理员通过 `cmd/bootstrap` 或 `make bootstrap-admin` 独立创建，不依赖迁移隐式创建弱密码账号。
- 生产环境通过 `APP_DB_PATH`、`APP_JWT_SECRET` 等环境变量覆盖配置。
- 迁移完成后应执行后端测试和 `go vet`，并确认 `captures`、菜单以及角色权限均符合当前模型。
