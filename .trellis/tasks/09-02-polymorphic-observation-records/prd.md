# 多态通用识别记录与身份层多模态演进 (Polymorphic Observation Records & Identity Layer Evolution)

## 1. Goal & Background

在 Argus 边缘 AI 视频分析与 RBAC 管理系统中，感知层已成功重构为多态通用的**「抓拍记录 (`captures`)」**，统一接入人脸、人体、机动车/车牌、非机动车等多模态视觉事实。

然而，系统的第二层——**「身份层（Identity / 识别记录）」** 目前存在结构性割裂：
1. **名不副实**：界面左侧菜单叫「识别记录」，但底层数据模型仍是单一的人脸比对表 (`face_observations`)，字段强耦合 `person_name`、`face_crop`、`candidates_json`（人脸 Top-5 底库候选）；
2. **车牌/车辆识别被边缘化**：车牌识别记录 (`plate_observations`) 虽有数据和独立 API，但在统一的记录中心内无入口；
3. **底库比对概念未统一**：人脸识别（1:N 人员底库检索）与车牌通行识别属于平行的身份凭证，需要统一的多态抽象。

本任务旨在完成**身份层（识别记录）的多态通用化重构**，采用 **「逻辑多态聚合 + 渐进式平滑路由演进 (Hybrid A+B)」** 架构：
- 打造统一的 **多态通用识别中心 (Polymorphic Observation Hub)**；
- 建立规范标准的三层记录体系：**抓拍记录 (`/record/capture`) $\to$ 识别记录 (`/record/observation`) $\to$ 告警记录 (`/record/alarm`)**；
- 同时在后端 API、前端路由及 RBAC 权限层对既有 `/record/face` 保持 100% 向后兼容与无缝跳转。

---

## 2. 领域模型与核心事实 (Ubiquitous Language & Confirmed Facts)

### 2.1 三层体系架构定位

| 体系分层 | 核心模型 / 接口 | 核心定位与业务语义 | 数据生成机制 | 业务载体 |
| :--- | :--- | :--- | :--- | :--- |
| **感知层 (Perception)** | **`captures`** | 全量视觉过客事实底座（*画面中看到了什么？*） | 任意有效目标（人体/人脸/车辆）检测触发 | `face`, `person`, `vehicle`, `non_motor` |
| **身份层 (Identity)** | **`ObservationHub`** (`/api/record/observations`) | 静态底库比对与通行凭证（*是谁 / 哪辆车？*） | 抓拍目标特征命中人员底库或车牌识别通行 | `person`（人员比对）, `vehicle`（车辆/车牌比对） |
| **事件层 (Event)** | **`alarm_records`** | 规则与安全违规（*发生了什么违规？*） | 目标触发空间/合规防范规则（越界、明火、未戴安全帽等） | 行为与安全事件 |

### 2.2 架构决策汇总 (Confirmed Technical Decisions)

1. **后端数据层：逻辑多态聚合 (Logical Polymorphism Hub)**
   - 底层保留 `face_observations` 与 `plate_observations` 独立的高性能物理存储与 C++ IPC 管道隔离；
   - 新增 `ObservationService`，提供统一的 `/api/record/observations`；
   - `targetType=all` 时执行时间倒序联合分页归并；`targetType=person|vehicle` 时直接下推专有仓储查询，最大化利用索引。
2. **前端与路由：规范路由 + 渐进式平滑过渡 (Hybrid A+B)**
   - 规范路由路径演进为 `/record/observation`，组件位于 `views/record/observation/index.vue`；
   - 既有 `/record/face` 路由配置重定向或别名平滑跳转至 `/record/observation`；
   - 数据库菜单通过幂等迁移脚本升级为 `RecordObservation`，原有角色绑定关系 (`role_menus`) 自动平滑继承；
   - 后端路由同时注册 `/api/record/observations` 与 `/api/record/faces`，权限码统一为 `record:observation`（兼容 `record:face`）。

---

## 3. Requirements (需求与功能规划)

### R1. 后端多态聚合服务与 API 建设 (Backend Services & APIs)
- **B1.1**: 新增 `app/internal/service/observation_service.go`：
  - 实现 `ObservationService`，对外提供统一的 `ListPage`、`GetDetail` 与 `ReadImageStream`；
  - 支持按 `targetType` (`all` | `person` | `vehicle`)、`cameraId`、`startTime` / `endTime`、`keyword` (模糊匹配人员姓名、工号、车牌号码) 联合查询；
- **B1.2**: 新增 `app/internal/api/observation.go` 并注册路由 `/api/record/observations`（支持详情与原图/特写图片流）；
- **B1.3**: Wire 依赖注入装配 `ObservationService` 与 `ObservationHandler`，并在 `router.go` 中完成注册；
- **B1.4**: 菜单与迁移：新增 `000042_seed_generic_observation_menu.up.sql` / `.down.sql`，同步更新 `model/seed.go`。

### R2. 前端多态通用识别中心交互 (Frontend UI)
- **F2.1**: API 契约：新增 `ui/apps/web-antd/src/api/core/observation.ts`，定义多态 `ObservationItem` 与查询入参；
- **F2.2**: 路由与国际化：
  - 更新菜单路由为 `/record/observation`，保留 `/record/face` 重定向；
  - 完善中/英/繁多语言包（`routes.json`, `record.json`, `ops.json` 中的 `record.observation.*`）；
- **F2.3**: 识别记录主页面 `views/record/observation/index.vue`：
  - 顶部常驻 **方案 A 胶囊分类 (Segmented)**：`[ 全部 ]` `[ 👤 人员识别 ]` `[ 🚗 车辆识别 ]`；
  - 智能输入框自适应切换 Placeholder（切人员显示 `"输入姓名或工号"`，切车辆显示 `"输入车牌号码"`，全部显示 `"输入姓名、工号或车牌号"`）；
  - 表格根据当前选中的目标类型自适应渲染特写列（人脸特写 vs 车牌特写）与主体标识（人员姓名 vs 车牌号 Tag）；
- **F2.4**: 多态识别详情抽屉 (`ObservationDetailDrawer.vue`)：
  - 人员识别：展示大图标注 + Top-5 候选底库比对分析表 + 人脸样本比对；
  - 车辆识别：展示车辆全景 + 车牌高清特写 + 车辆多维属性面板（车牌颜色、车辆类型、OCR 置信度等）。

---

## 4. Acceptance Criteria (验收标准)

- [ ] **多态聚合分页与检索**：
  - 在「全部」模式下，能按时间倒序混排展示人员识别记录与车牌识别记录，总条数与联合分页精确无误；
  - 切换到「人员识别」仅展示人员比对记录；切换到「车辆识别」仅展示车牌识别记录；
  - 关键词搜索在人员模式下命中人员姓名/工号，在车辆模式下命中车牌号码，在全部模式下同时生效。
- [ ] **多态详情与大图预览**：
  - 点击人员识别项，弹出详情抽屉展示全景图、人脸特写与 Top-5 底库候选比对分析；
  - 点击车辆识别项，弹出详情抽屉展示全景图、车牌特写、车牌颜色、类型与 OCR 置信度。
- [ ] **平滑兼容性**：
  - 访问 `/record/face` 自动平滑重定向至 `/record/observation`；
  - 既有角色的权限自动继承，无权限丢失；
  - 旧版 API 路径保留有效。
- [ ] **测试验证**：Go 后端单元测试覆盖联合分页与单类型下推查询，前端 `pnpm check` 与单测全绿通过。

---

## 5. Out of Scope (本次范围外)

- 物理表结构合并（已明确采用逻辑多态聚合架构）；
- 车辆黑白名单与复杂计费管理系统（后续独立专题演进）。
