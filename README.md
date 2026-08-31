# Argus

`Argus` 是一个面向边缘计算场景的高性能 AI 视频分析与 RBAC 管理系统。

后端基于 **Go 1.26 + Gin + GORM + Wire** 构建，引擎基于 **C++20 + ZLMediaKit + 动态 C ABI 算法插件**，前端基于 **Vue 3 + Vite + Ant Design Vue + vben-admin 5.7** 定制，采用后端路由驱动（Backend Access Mode）与 JWT 双 Token 认证，支持一键容器化部署。

---

## 特性亮点

- **技术栈纯粹**：Go 1.26 后端分层架构（`model` / `repository` / `service` / `api`）+ google/wire 依赖注入。
- **多数据库支持**：GORM 双驱动，一键切换 **PostgreSQL** 或 **MySQL**，支持软删除唯一索引兼容与幂等 Seed 数据初始化。
- **RBAC 权限管理**：用户管理、角色管理、菜单与按钮权限分配、部门组织架构。
- **自动化操作日志**：全自动中间件拦截写操作，敏感字段脱敏，请求耗时与操作明细追溯。
- **个人中心与国际化**：支持用户修改个人资料与密码、全站中/繁/英三语切换。
- **轻量开箱即用**：无需 Redis，双 Token 轮换与黑名单基于数据库维护。
- **边缘 AI 视频分析**：基于 C++20 引擎与标准 C ABI 算法插件体系，支持 Core ML (macOS Apple Silicon)、Rockchip RKNN (RK3576/RK3588) 等多硬件后端与端边算力协同。
- **完整部署生态**：提供多阶段 Dockerfile、Nginx 反代配置及 Docker Compose 一键拉起环境。

---

## 环境要求

- **Go**：>= 1.26
- **Node.js**：>= 22.18.0 或 >= 24.12.0
- **pnpm**：>= 11.0.0
- **数据库**：PostgreSQL >= 14 或 MySQL >= 8.0
- **Docker & Docker Compose**（可选，容器化部署使用）

---

## 默认账号

- **用户名**：`admin`
- **密码**：`admin123`

> ⚠️ 生产环境部署后请立即登录并在个人中心修改默认密码！

---

## 快速开始（本地开发）

### 1. 克隆代码

```bash
git clone <repository-url>
cd argus
```

### 2. 下载模型权重文件

由于 AI 算法模型权重文件较大，未直接提交到 Git 仓库。请从以下 Google Drive 地址下载模型文件并解压放置到对应的算法包目录中：

- **下载地址**：[Google Drive 模型权重下载链接](https://drive.google.com/drive/folders/13eXexmJK5bIsFo-qAb-3Npb6QNt2LfSQ?usp=sharing)
- **对应放置路径**：
  - **macOS (arm64) YOLO26n**：`algo-packages/macos/arm64/yolo26n/model/yolo26n.mlpackage`（或 `weights/`）
  - **macOS (arm64) 人脸识别**：
    - `algo-packages/macos/arm64/face_recognition/model/yolov8n.mlpackage`
    - `algo-packages/macos/arm64/face_recognition/model/scrfd_10g_bnkps.mlpackage`（以及 `weights/scrfd_10g_bnkps.onnx`）
    - `algo-packages/macos/arm64/face_recognition/model/glintr100.mlpackage`（以及 `weights/glintr100.onnx`）
  - **RK3576 YOLOv8n**：
    - `algo-packages/rknn/rk3576/yolov8n/model/yolov8n.rknn`
    - `algo-packages/rknn/rk3576/yolov8n/model/yolov8n.onnx`

### 3. 启动数据库

在本地启动 PostgreSQL（默认端口 `5432`）或 MySQL（默认端口 `3306`），并创建数据库 `niko_vue_admin_go`。

### 4. 启动后端 (`app`)

```bash
cd app

# 复制或根据本地环境修改配置文件
# configs/config.yaml

# 运行测试
make test

# 启动开发服务器（支持 air 热重载或直接运行）
make dev
# 或者
make dev-raw
```

- 后端服务默认监听：`http://localhost:8000`
- Swagger 文档地址：`http://localhost:8000/swagger/index.html`

### 5. 启动前端 (`ui`)

打开新终端窗口：

```bash
cd ui

# 安装依赖（仅限 pnpm）
pnpm install

# 启动 Ant Design 业务端
pnpm dev:antd
```

- 前端访问地址：`http://localhost:5320`（通过 Vite 代理自动转发 `/api` 到 `http://localhost:8000`）

---

## Docker Compose 容器化部署

项目中提供了一键编排部署配置，包含数据库、后端 API 与 Nginx 托管前端。

### 1. 构建前端静态资源

```bash
cd ui
pnpm install
pnpm run build:antd
```

### 2. 一键启动 Docker 编排

```bash
cd deploy
docker compose up -d --build
```

### 3. 访问系统

- 打开浏览器访问：`http://localhost`
- 后台 API 与 Swagger 自动由 Nginx 进行同域反代。

---

## 目录结构

```text
argus/
├── algo-packages/               # 独立算法插件包 (macOS CoreML, RKNN 等)
├── app/                          # Go 后端工程 (module argus/app)
│   ├── cmd/api/                  # 入口与 Wire DI 装配 (main.go, wire.go)
│   ├── configs/                  # 配置文件 (config.yaml)
│   ├── docs/                     # Swagger OpenAPI 文档
│   ├── internal/
│   │   ├── api/                  # Gin Handlers (参数校验、调用 Service)
│   │   ├── middleware/           # 中间件 (Auth 鉴权、RBAC 权限、Oplog 审计日志等)
│   │   ├── model/                # GORM 数据模型与 Seed 初始化
│   │   ├── pkg/                  # 公共工具包 (config, db, errno, jwt, mask, response)
│   │   ├── repository/           # 数据访问层 (BaseRepo 泛型封装与各实体 Repo)
│   │   ├── router/               # 路由与权限码注册
│   │   └── service/              # 业务逻辑层
│   ├── migrations/               # 版本化 SQL 迁移脚本
│   ├── Makefile                  # 构建、测试、文档生成脚本
│   └── Dockerfile                # 多阶段构建镜像定义
├── engine/                       # C++20 媒体推理引擎与管线调度
├── sdk/                          # 共享 C ABI 接口与 SDK 头文件
├── ui/                           # 前端 Monorepo
│   ├── apps/
│   │   └── web-antd/             # Ant Design Vue 主业务应用
│   │       ├── src/api/          # API 客户端与请求定义
│   │       ├── src/views/system/ # 用户/角色/菜单/部门/操作日志页面
│   │       └── src/views/profile/# 个人中心页面
│   └── packages/                 # vben 核心基础设施包
├── deploy/                       # 部署配置
│   ├── docker-compose.yml        # 一键容器编排
│   └── nginx.conf                # Nginx 静态托管与 API 反代配置
└── README.md                     # 项目说明文档
```

---

## 新增业务模块指南

新增一个业务模块只需三步：

1. **后端数据与业务层实现**：
   - 在 `app/internal/model/` 定义 GORM 实体模型；
   - 在 `app/internal/repository/` 定义 Repository 接口及实现（组合 `BaseRepo`）；
   - 在 `app/internal/service/` 编写业务逻辑；
   - 在 `app/internal/api/` 实现 Handler 并声明 Swagger 注释；
   - 在 `app/internal/router/router.go` 注册路由分组与权限码；
   - 运行 `cd app && make wire && make swagger` 更新依赖注入与接口文档。

2. **配置菜单与权限**：
   - 在 `app/internal/model/seed.go` 添加菜单路由与按钮权限定义（或通过系统「菜单管理」页面动态添加），指定前端组件路径（如 `/system/example/index`）。

3. **前端页面开发**：
   - 在 `ui/apps/web-antd/src/api/` 编写接口调用函数；
   - 在 `ui/apps/web-antd/src/views/` 创建对应的 Vue 页面组件；
   - 使用 `useVbenVxeGrid` 或 Ant Design 组件进行视图渲染，重新登录后即可在侧边栏看到新模块。

---

## 常用命令汇总

### 后端 (app/)

- `make test`：运行所有单元测试与集成测试
- `make vet`：执行 Go 静态代码检查
- `make wire`：重新生成 Wire 依赖注入代码
- `make swagger`：生成 Swagger API 文档
- `make build`：编译输出可执行文件到 `bin/api`

### 前端 (ui/)

- `pnpm dev:antd`：启动本地开发服务
- `pnpm run build:antd`：构建生产包
- `pnpm check`：执行全量静态质量检查（TypeScript 类型、ESLint、循环依赖、拼写检查）
- `pnpm test:unit`：运行单元测试
