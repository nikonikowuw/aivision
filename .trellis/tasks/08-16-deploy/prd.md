# Dockerfile + docker-compose + nginx + README

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

补齐部署物与文档，让 clone 者按 README 即可本地开发、容器部署；本项目为收尾任务，完成后父任务整体验收。

## 依赖

- 全部 10 个前置子任务完成。

## Requirements

- `app/Dockerfile`：多阶段构建（golang:1.26-alpine → alpine，CGO_ENABLED=0）。
- `deploy/docker-compose.yml`：mysql:8（初始化库/账号 + healthcheck）、server（依赖 mysql）、web（nginx:alpine，托管 dist + 反代 `/api` → server:8000）。
- `deploy/nginx.conf`：静态托管 + `/api` 反代。
- 根 `README.md`：项目简介、环境要求（Go/Node/pnpm/MySQL）、本地启动步骤（后端 + 前端）、docker-compose 部署步骤、默认账号、目录结构、新增模块指南（四层 + 菜单 seed + 前端页面三步）。

## Acceptance Criteria

- [ ] 父 AC-14：`docker compose up -d` 后访问 web 端口可打开完整后台并完成 admin 登录。
- [ ] README 按步骤从零执行可跑通（go 与 pnpm 两段）。
- [ ] 父任务全部 AC-1 ~ AC-16 回归通过（最终集成验收，含 `go test ./...`、`pnpm check`）。

## Out of Scope

- CI、生产级容器编排（k8s 等）
