# 执行计划：5 个业务页面 views/system/*

> 对应任务：`.trellis/tasks/08-16-frontend-pages/`

## 执行顺序

1. **Step 1: API 层与类型定义**
   - 补全 `src/api/core/user.ts`（支持分页查询、CRUD、重置密码、角色分配、启停用）
   - 新增 `src/api/core/role.ts`（支持分页查询、CRUD、菜单权限分配）
   - 补全 `src/api/core/menu.ts`（支持全量菜单树查询、CRUD）
   - 新增 `src/api/core/dept.ts`（支持部门树查询、CRUD）
   - 新增 `src/api/core/log.ts`（支持操作日志分页、详情查询）
   - 在 `src/api/core/index.ts` 统一重导出
   - 验证：`cd ui && pnpm run check:type`

2. **Step 2: 国际化词条配置**
   - 在 `src/locales/langs/zh-CN/page.json` 与 `en-US/page.json` 中增加 `routes.system.*` 相关词条与页面通用文本
   - 验证：多语言文件语法无误

3. **Step 3: 实现部门管理页面 (`views/system/dept/index.vue`)**
   - 实现部门树形表格与增删改弹窗表单
   - 验证：类型检查与页面结构

4. **Step 4: 实现菜单管理页面 (`views/system/menu/index.vue`)**
   - 实现菜单树形表格（支持 catalog/menu/button 展示）与新增/编辑弹窗表单
   - 验证：类型检查与页面结构

5. **Step 5: 实现角色管理页面 (`views/system/role/index.vue`)**
   - 实现角色分页表格、增删改表单
   - 实现分配菜单权限弹窗（Tree 复选框，回显与保存）
   - 验证：类型检查与页面结构

6. **Step 6: 实现用户管理页面 (`views/system/user/index.vue`)**
   - 实现用户列表表格、条件查询（含部门与状态）
   - 实现新增/编辑用户弹窗、重置密码确认、角色分配弹窗、启停用控制
   - 验证：类型检查与页面结构

7. **Step 7: 实现操作日志页面 (`views/system/log/index.vue`)**
   - 实现日志分页列表与多条件过滤（时间段、操作人、模块等）
   - 实现日志详情 Drawer/Modal，脱敏展示与请求元信息查看
   - 验证：类型检查与页面结构

8. **Step 8: 全量质检与静态检查**
   - 运行 `cd ui && pnpm check`
   - 确保 `check:circular`, `check:dep`, `check:type`, `check:cspell` 全部通过
