# 告警记录模块执行计划 (implement.md)

## Ordered Implementation Steps

### Phase 1: 算法包与 Engine 契约校准
- [ ] 1.1 修改 macOS/arm64 YOLOv8n 示例包（`algo_entry.cpp`）：
  - 增加 `track_alarm_cooldown_` 机制（默认 5s 冷却）；
  - 将多目标合并回调改为单目标独立回调；
  - 截图请求改为全景大图 `[0,0,1,1]`。
- [ ] 1.2 同步修改 RKNN/rk3576 YOLOv8n 示例包（`algo_entry.cpp`）。
- [ ] 1.3 运行算法包单测与编译验证。

### Phase 2: 数据库迁移与后端 Model/Repo/Service
- [ ] 2.1 创建 `000021_add_alarm_records.up.sql` / `down.sql`。
- [ ] 2.2 创建 `000022_seed_record_alarm_menu.up.sql` / `down.sql`（配置「智能记录/告警记录」菜单及权限码）。
- [ ] 2.3 新增 `app/internal/model/alarm_record.go`。
- [ ] 2.4 新增 `app/internal/repository/alarm_record.go` 并实现分页查询、按 event_id/image_id 检索方法。
- [ ] 2.5 新增 `app/internal/service/alarm_record.go` 实现业务逻辑与 DTO 转换。

### Phase 3: Engine IPC 接入与图片读取
- [ ] 3.1 在 `app/internal/service/report_adapter.go` 实现 `AcceptAlarm`（幂等落库）。
- [ ] 3.2 在 `app/internal/service/report_adapter.go` 实现 `ReconcileOrphanImages`（孤儿对账）。
- [ ] 3.3 新增 `app/internal/handler/alarm_record.go`：
  - `GET /api/record/alarms`（列表与组合筛选）
  - `GET /api/record/alarms/:id`（详情）
  - `GET /api/record/images/:id`（图片安全流输出）
- [ ] 3.4 注册路由与 Wire 依赖注入。

### Phase 4: 前端界面开发
- [ ] 4.1 新增 API 定义 `ui/apps/web-antd/src/api/record/alarm.ts`。
- [ ] 4.2 创建告警记录主视图 `ui/apps/web-antd/src/views/record/alarm/index.vue`（查询表单 + 表格 + 分页）。
- [ ] 4.3 创建告警详情抽屉/弹窗 `ui/apps/web-antd/src/views/record/alarm/components/AlarmDetailModal.vue`（包含全景图底图 + Canvas/SVG 坐标叠画标注）。
- [ ] 4.4 配置前端路由与多语言 i18n（`zh-CN`, `en-US`, `zh-TW`）。

### Phase 5: 质量与集成验证
- [ ] 5.1 运行 Go 单元测试：`make -C app test`。
- [ ] 5.2 运行前端类型与代码检查：`pnpm --filter web-antd check`。
- [ ] 5.3 验证数据库迁移与回滚：`make -C app migrate-version`。
