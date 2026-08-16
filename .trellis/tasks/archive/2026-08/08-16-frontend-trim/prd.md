# vben 裁剪只留 web-antd

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

把 `ui/` 的 vben 5.7.0 monorepo 裁剪为单应用 `apps/web-antd`，删掉其余应用与 mock，保证安装、构建、检查全通过。

## 依赖

无（与后端模块无依赖，可与后端并行）。

## Requirements

- 删除 `apps/web-ele`、`apps/web-naive`、`apps/web-tdesign`、`apps/backend-mock`。
- 清理 `pnpm-workspace.yaml` 引用、根 `package.json` 相关脚本（build:ele/naive/tdesign/mock 等）、`.vscode/vben-admin.code-workspace` 引用、docs 中 mock 引用（尽力而为）。
- 保留 web-antd 所需全部 packages（@vben/* 基础设施），不裁剪 packages 内部。
- 若裁剪引发连锁构建问题且无法在合理时间内解决，降级：恢复全量 monorepo、仅后续任务使用 web-antd（见父 design.md §10）。

## Acceptance Criteria

- [ ] 父 AC-1：`pnpm install` 干净完成；`pnpm build`（web-antd）成功；`pnpm check` 通过。
- [ ] 仓库中不再存在 web-ele/web-naive/web-tdesign/backend-mock 目录及其 workspace 引用残留。

## Out of Scope

- 对接后端、业务页面（frontend-integration / frontend-pages）
