# 多态通用识别记录实现计划 (Implementation Plan)

## 1. Ordered Implementation Checklist

### Phase 1: 后端服务与路由挂载 (Backend Services & Router)
- [ ] **B1.1 聚合服务开发**：
  - 新建 `app/internal/service/observation_service.go`；
  - 实现 `ObservationService` 接口（`ListPage` 联合分页归并、`GetDetail`、`ReadImageStream`）；
  - 编写 `app/internal/service/observation_service_test.go` 单元测试。
- [ ] **B1.2 API 控制器与路由挂载**：
  - 新建 `app/internal/api/observation.go`；
  - 注册 `ObservationHandler` 到 `app/cmd/api/wire.go` 并执行 `make wire`；
  - 在 `app/internal/router/router.go` 中注册 `/api/record/observations` 路由与权限中间件。
- [ ] **B1.3 数据库迁移与菜单种子**：
  - 新增 `app/migrations/000042_seed_generic_observation_menu.up.sql` / `.down.sql`；
  - 更新 `app/internal/model/seed.go` 中 `seedMenuTree` 的 `RecordObservation` 定义。

### Phase 2: 前端多态识别中心构建 (Frontend UI)
- [ ] **F2.1 API 契约与多语言**：
  - 新增 `ui/apps/web-antd/src/api/core/observation.ts`；
  - 在 `zh-CN`, `zh-TW`, `en-US` 的 `routes.json`, `record.json`, `ops.json` 中配置 `record.observation` 完整词条。
- [ ] **F2.2 主页面与抽屉开发**：
  - 新建 `ui/apps/web-antd/src/views/record/observation/index.vue`（Segmented 胶囊 + 自适应表格）；
  - 新建 `ui/apps/web-antd/src/views/record/observation/components/ObservationDetailDrawer.vue`（人员 Top-5 分析 vs 车辆属性面板）；
  - 在 `ui/apps/web-antd/src/views/record/face/index.vue` 保留向后兼容别名或重定向。

### Phase 3: 整体验证与质量检查 (Verification & Quality Gate)
- [ ] **V3.1 后端验证**：执行 `make migrate-up`、`go test ./...` 与 `go vet ./...`；
- [ ] **V3.2 前端验证**：执行 `pnpm check`（circular / dep / typecheck / cspell）与 `pnpm test:unit`；
- [ ] **V3.3 交互冒烟**：验证在「全部」、「人员识别」、「车辆识别」各分类下的查询与详情弹出流畅无异常。

---

## 2. Validation Commands

```bash
# 后端测试
cd app && go test ./... && go vet ./...

# 前端检查
cd ui && pnpm check && pnpm test:unit
```
