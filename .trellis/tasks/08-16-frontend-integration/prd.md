# vite proxy + accessMode + auth.ts + 登录页清理 + 登录联调

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

web-antd 与 Go 后端打通认证与动态路由：登录、刷新、登出、菜单驱动的路由生成全部可用。本任务结束后浏览器 admin/admin123 登录即可看到后端下发的菜单导航。

## 依赖

- `08-16-frontend-trim`（web-antd 可构建）。
- `08-16-backend-auth`、`08-16-backend-menu`（接口就绪）。
- 联调目标端口：Go 后端 `:8000`。

## Requirements

- `preferences.ts`：`accessMode: 'backend'`。
- `vite.config.ts`：proxy `/api` → `http://localhost:8000/api`（**删除 rewrite**）。
- `src/api/core/auth.ts`：login/refresh/logout/getAccessCodesApi 对齐后端契约（refresh 用 withCredentials cookie 模式）。
- 登录页移除 `ThirdPartyLogin` 引用（隐藏微信/QQ/GitHub/Google/钉钉图标，AC-16）。
- `user/info`、`menu/all` 请求路径与后端一致（已一致则不动）。
- 菜单 title 用后端返回的中文字符串渲染。

## Acceptance Criteria

- [ ] 父 AC-4/AC-5 前端侧：admin/admin123 登录成功，进入首页，左侧菜单显示「系统管理」全部模块且可点击进入（页面内容由 frontend-pages 提供，本任务允许空白页占位）。
- [ ] access 过期后前端自动刷新并重试成功（可缩短后端 ttl 验证）。
- [ ] 登出回登录页；登录页无任何第三方登录入口（AC-16）。
- [ ] 无权限用户登录后菜单不含无权限项。
- [ ] `pnpm check` 通过。

## Out of Scope

- 业务页面实现（frontend-pages）
