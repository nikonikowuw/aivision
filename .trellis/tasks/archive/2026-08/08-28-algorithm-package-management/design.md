# Design: 算法包管理 (Algorithm Package Management)

## 1. 架构与技术选型

### 1.1 数据流架构
```
+-------------------------------------------------------------+
|                      Vue 3 前端                             |
|  - 算法包卡片列表                                           |
|  - 上传模态框 (Upload Modal)                                |
|  - 版本抽屉 (Version Drawer)                                |
|  - 参数规范抽屉 (Config Schema Drawer)                      |
+------------------------------+------------------------------+
                               | HTTP POST /api/v1/algorithms/upload
+------------------------------v------------------------------+
|                    Go 后端 (app/)                           |
|  1. gin-multipart 接收算法包 .tar.gz                        |
|  2. 解压到 safe staging 临时目录:                            |
|     `data/tmp/packages/<uuid>/`                             |
|     (执行 ZipSlip/TarSlip 路径逃逸检测)                     |
|  3. 读取解压后 manifest.json 与 config.schema.json          |
|  4. 通过 engineipc.EngineClient 调用 Engine:                 |
|     InstallPackageRequest{PackagePath: staging_path}        |
|  5. 响应处理:                                               |
|     - 若 Engine 返回 code != "", 清理临时目录并返回错误码    |
|     - 若 Engine 成功, GORM 开启事务:                        |
|       * upsert `algorithms` 基础信息与当前 active_version   |
|       * upsert `algorithm_versions` 版本元数据与 Schema     |
|  6. 清理 staging 临时目录，向前端返回统一成功格式           |
+------------------------------+------------------------------+
                               | UDS gRPC (engine.sock)
+------------------------------v------------------------------+
|                    C++ Engine                               |
|  1. UdsServer.InstallPackage                                |
|  2. 调用 package_validator 子进程隔离执行安全自测与校验      |
|  3. 拷贝到 var/packages/<algo_id>/<version>/                |
|  4. 写入 var/packages/active/<algo_id>.version              |
+-------------------------------------------------------------+
```

### 1.2 关键设计决策
1. **安装时解压与目录校验**：
   - 算法包分发统一采用 `.tar.gz` 格式。Go 后端负责接收并解包至暂存目录，校验包含 `manifest.json` 与声明的入口文件后再转交 Engine 验证。这隔离了 Engine 面对不可信二进制流的解压风险。
2. **元数据所有权**：
   - Engine 是底层物理包的执行方，Go 后端是业务元数据的权威。安装成功后，Go 后端将 manifest 中的 `resource_profile`（fps 档位）、`runtime_constraints` 和 `config.schema.json` 内容结构化解析入库，使得前端后续配置分析任务时无需频繁跨进程读取 Engine 物理文件。
3. **版本切换与回滚**：
   - 调用 Engine 的 `RollbackPackageRequest{AlgorithmId, TargetVersion}`；
   - 引擎更新 `active/<algo_id>.version` 后，Go 更新 `algorithms.active_version` 与 `algorithm_versions.is_active`。
4. **安全卸载**：
   - 调用 Engine `UninstallPackageRequest{AlgorithmId, Version}`；
   - 引擎已内置引用检查（若当前有正在运行的实例引用此包，返回 `PACKAGE_IN_USE`）；Go 侧同时在后续分析任务表建立后做双重业务校验。

---

## 2. 数据库设计 (PostgreSQL)

### 2.1 迁移脚本：`000017_add_algorithm_packages.up.sql`
```sql
CREATE TABLE IF NOT EXISTS algorithms (
    id BIGSERIAL PRIMARY KEY,
    algorithm_id VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(128) NOT NULL,
    algorithm_type VARCHAR(32) NOT NULL,
    alarm_type_id VARCHAR(64) NOT NULL DEFAULT '',
    active_version VARCHAR(32) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS algorithm_versions (
    id BIGSERIAL PRIMARY KEY,
    algorithm_id VARCHAR(64) NOT NULL,
    version VARCHAR(32) NOT NULL,
    platform_id VARCHAR(64) NOT NULL,
    min_adapter_version VARCHAR(32) NOT NULL DEFAULT '',
    package_root VARCHAR(255) NOT NULL DEFAULT '',
    fps_tiers JSONB NOT NULL DEFAULT '[]'::jsonb,
    config_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    manifest_raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    package_size_bytes BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT uk_algo_version UNIQUE (algorithm_id, version)
);

CREATE INDEX IF NOT EXISTS idx_algo_versions_algo_id ON algorithm_versions(algorithm_id);
```

### 2.2 菜单与权限种子：`000018_seed_algorithm_menu.up.sql`
- 父菜单：`ai` 目录（AI 算法）
- 菜单：`ai:algorithms`（算法包管理，组件路径 `/ai/algorithms/index`）
- 按钮权限：
  - `ai:algorithms:query`
  - `ai:algorithms:upload`
  - `ai:algorithms:activate`
  - `ai:algorithms:uninstall`

---

## 3. Go 后端模块设计

1. **`app/internal/model/algorithm.go`**：
   - `Algorithm` 与 `AlgorithmVersion` GORM 模型定义。
2. **`app/internal/repository/algorithm.go`**：
   - 算法与版本数据的增删改查、事务更新 active 状态。
3. **`app/internal/service/algorithm.go`**：
   - `AlgorithmService` 接口与实现；
   - TarGz 安全解压器（防目录穿越漏洞）；
   - 对接 `engineipc.EngineClient`。
4. **`app/internal/api/v1/algorithm.go`**：
   - 暴露 RESTful 路由处理函数。
5. **`app/cmd/api/wire.go`**：
   - 注册并绑定依赖注入。

---

## 4. 前端界面设计

1. **路由定义**：`apps/web-antd/src/router/routes/modules/ai.ts`
2. **API 模块**：`apps/web-antd/src/api/ai/algorithm.ts`
3. **主页面**：`apps/web-antd/src/views/ai/algorithms/index.vue`
   - 卡片式展示每个算法，卡片上显示：算法名称、算法标识、类型标签、目标平台徽章、当前激活版本、FPS 档位标签、操作按钮（版本历史、参数规范）。
   - 顶部提供“安装算法包”按钮。
4. **弹窗与抽屉组件**：
   - `UploadModal.vue`：上传 `.tar.gz` 文件，支持拖拽、平台提醒与安装自测状态展示；
   - `VersionsDrawer.vue`：查看历史版本，提供“设为激活版本”、“卸载此版本”操作；
   - `SchemaModal.vue`：友好预览该算法版本的参数配置规范（表格呈现参数 key、标题、类型、默认值、必填项、校验说明）。
