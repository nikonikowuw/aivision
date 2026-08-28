# Implementation Plan: 算法包管理 (Algorithm Package Management)

## 1. 阶段目标与执行顺序

```
Phase 1: 数据库与模型定义 (Migrations & Models)
  - 1.1 编写 000017_add_algorithm_packages.up/down.sql
  - 1.2 编写 000018_seed_algorithm_menu.up/down.sql
  - 1.3 编写 model.Algorithm 与 model.AlgorithmVersion
  - 1.4 执行本地测试迁移验证

Phase 2: 后端数据访问与业务层 (Repository & Service)
  - 2.1 编写 repository/algorithm.go (CRUD & 事务)
  - 2.2 编写 service/algorithm_tar.go (安全解包、manifest/schema 解析)
  - 2.3 编写 service/algorithm.go (业务逻辑 + EngineClient UDS 交互)
  - 2.4 编写针对安全解压与安装流程的单元测试

Phase 3: 后端接口与路由注入 (Handler, Router & Wire)
  - 3.1 编写 api/v1/algorithm.go (HTTP Handler)
  - 3.2 注册路由并在 wire.go 中绑定依赖注入
  - 3.3 运行 make wire 并执行整体单元测试与静态检查

Phase 4: 前端开发 (UI Views, API & Components)
  - 4.1 编写 api/ai/algorithm.ts 请求层定义
  - 4.2 配置前端 AI 路由与 i18n 多语言
  - 4.3 实现 views/ai/algorithms/index.vue 算法列表卡片页
  - 4.4 实现 UploadModal.vue (算法上传与验证安装)
  - 4.5 实现 VersionsDrawer.vue (版本切换与卸载)
  - 4.6 实现 SchemaModal.vue (算法参数规范预览)
  - 4.7 运行 pnpm check 校验前端代码规范

Phase 5: 全链路联调与回归验证 (End-to-End Verification)
  - 5.1 本地对真实 yolov8n 算法包进行打包含压缩
  - 5.2 启动后端，测试真实算法包上传 -> 后端暂存 -> Engine 验证安装 -> 数据库入库
  - 5.3 验证版本切换、Schema 查看与卸载拦截逻辑
  - 5.4 完善文档与验收归档
```

## 2. 检查项清单

- [ ] 数据库迁移无报错，且 down 脚本能完全清理
- [ ] Tar 解压严密防止 TarSlip 任意路径穿越
- [ ] EngineClient 错误码正确映射并向用户返回清晰诊断信息
- [ ] 前端 TypeScript 编译无类型报错，通过 oxlint
- [ ] 真实算法包可成功上传安装并在前端渲染
