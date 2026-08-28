# PRD: 算法包管理 (Algorithm Package Management)

## 1. 目标与价值
算法包管理是连接边缘硬件、推理引擎与上层业务分析任务的关键中枢。
本模块提供端到端的算法包生命周期管理：
1. **算法包上传与验证安装**：接收 `.tar.gz` 算法包，后端暂存后对接 Engine `InstallPackage`，经独立沙箱验证进程校验 manifest、SHA-256、符号表纯洁性与模型冒烟自测后落盘入库；
2. **多版本管理与激活/回滚**：支持同一算法多版本共存，展示各版本的资源档位（fps_tiers）与参数规范（config.schema.json），支持一键激活或回滚版本；
3. **安全卸载**：对接 Engine `UninstallPackage`，卸载前校验当前是否有分析任务正在引用该算法包；
4. **前端管理控制台**：在管理后台提供直观的算法包列表展示、上传安装向导、历史版本抽屉与 JSON Schema 参数预览。

---

## 2. 跨层契约与边界

### 2.1 依赖倒置与职责划分
- **Engine 职责**：算法包物理文件的存放管理（`var/packages/<algo_id>/<version>/`）、动态库校验与符号表审计（`package_validator`）、激活版本标记（`active/<algo_id>.version`）、IPC 接口暴露（`InstallPackage`, `UpgradePackage`, `RollbackPackage`, `UninstallPackage`）。
- **Go 后端职责**：业务元数据持久化权威（`algorithms`, `algorithm_versions`）、文件上传与安全解压暂存、协调 Engine UDS 客户端、提供 RESTful API 与 RBAC 权限控制。
- **前端职责**：展示算法卡片与版本列表、提供文件上传交互、渲染参数配置说明。

### 2.2 错误码映射
遵循项目约定，业务错误由统一响应体 `{code, message, data}` 承载，code=0 表示成功，非零错误码：
- `40001`：算法包格式错误或解压失败（非法路径、缺少 manifest 等）
- `40002`：算法包安装自测失败（对应 Engine 错误：PACKAGE_DLOPEN_FAILED, PACKAGE_ABI_MISSING, PACKAGE_METADATA_MISMATCH 等）
- `40003`：算法包正在使用中，禁止卸载（对应 Engine 错误：PACKAGE_IN_USE）
- `40004`：算法或版本不存在

---

## 3. 功能需求详细说明

### 3.1 数据库持久化设计
- **表 1：`algorithms`**
  - `id` (BIGSERIAL PRIMARY KEY)
  - `algorithm_id` (VARCHAR(64) UNIQUE NOT NULL) —— 算法唯一英文标识，如 `yolov8n`
  - `name` (VARCHAR(128) NOT NULL) —— 中文/展示名称
  - `algorithm_type` (VARCHAR(32) NOT NULL) —— 算法类型（`object_detection`, `face_recognition` 等）
  - `alarm_type_id` (VARCHAR(64) NOT NULL) —— 默认产生的告警事件类型，如 `object_detect`
  - `active_version` (VARCHAR(32) NOT NULL) —— 当前激活的版本号
  - `description` (TEXT)
  - `created_at`, `updated_at`, `deleted_at`

- **表 2：`algorithm_versions`**
  - `id` (BIGSERIAL PRIMARY KEY)
  - `algorithm_id` (VARCHAR(64) NOT NULL)
  - `version` (VARCHAR(32) NOT NULL)
  - `platform_id` (VARCHAR(64) NOT NULL) —— 目标平台，如 `macos-arm64-coreml`, `rk3576`
  - `min_adapter_version` (VARCHAR(32))
  - `package_root` (VARCHAR(255)) —— 引擎端物理路径
  - `fps_tiers` (JSONB NOT NULL DEFAULT '[]') —— 资源档位数组 `[{"fps": 5, "units": 60}, ...]`
  - `config_schema` (JSONB NOT NULL DEFAULT '{}') —— 配置规范 JSON Schema
  - `manifest_raw` (JSONB NOT NULL DEFAULT '{}') —— 完整 manifest 快照
  - `package_size_bytes` (BIGINT NOT NULL DEFAULT 0)
  - `is_active` (BOOLEAN NOT NULL DEFAULT FALSE)
  - `created_at`, `updated_at`, `deleted_at`
  - 唯一索引：`uk_algo_ver (algorithm_id, version)`

### 3.2 接口设计 (RESTful)
1. `GET /api/v1/algorithms`：算法列表（支持分页、按类型与关键字筛选）
2. `GET /api/v1/algorithms/:id/versions`：获取指定算法的所有已安装版本
3. `POST /api/v1/algorithms/upload`：上传算法包文件并触发安装与自测（multipart/form-data）
4. `PUT /api/v1/algorithms/:id/versions/:version/activate`：激活指定版本（调用 Engine Rollback/Upgrade 激活逻辑）
5. `DELETE /api/v1/algorithms/:id/versions/:version`：卸载指定版本（调用 Engine UninstallPackage）

### 3.3 前端页面
- 路由挂载在 “AI 算法” 模块下：“算法包管理” (`/ai/algorithms`)
- 页面卡片式/表格双重视图展示算法，提供状态标签、当前激活版本、目标平台与资源档位展示
- 点击版本可查看详细参数规范（Schema 树/表格格式）
- 上传弹窗提供文件选择、平台兼容性提示与自测报错友好提示

---

## 4. 验收标准
1. **自动化测试**：
   - Go 单测覆盖上传解压路径遍历防护、非法 tar 拦截、Engine RPC 错误转译；
   - 数据库迁移支持 `migrate-up` 与 `migrate-down` 且幂等；
2. **端到端契约**：
   - 上传本地真实的 `yolov8n` 算法包能够成功安装，并在数据库与前端展示出正确的 Schema 与 fps_tiers；
   - 尝试卸载已在运行的任务引用的包时返回明确错误提示；
3. **前端代码质量**：
   - 遵循 Ant Design Vue 与 Vben Admin 5.7 风格，通过 `pnpm check`（oxlint, typecheck 等）。
