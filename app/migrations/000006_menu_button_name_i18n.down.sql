-- 000006_menu_button_name_i18n.down.sql
-- 回滚：按钮 name 由 i18n key 恢复为中文展示名。

UPDATE menus SET name = '新增用户' WHERE type = 'button' AND permission = 'system:user:add'           AND deleted_at = 0;
UPDATE menus SET name = '编辑用户' WHERE type = 'button' AND permission = 'system:user:edit'          AND deleted_at = 0;
UPDATE menus SET name = '删除用户' WHERE type = 'button' AND permission = 'system:user:delete'        AND deleted_at = 0;
UPDATE menus SET name = '重置密码' WHERE type = 'button' AND permission = 'system:user:reset-password' AND deleted_at = 0;
UPDATE menus SET name = '分配角色' WHERE type = 'button' AND permission = 'system:user:assign-role'    AND deleted_at = 0;
UPDATE menus SET name = '启停用'   WHERE type = 'button' AND permission = 'system:user:status'        AND deleted_at = 0;
UPDATE menus SET name = '新增角色' WHERE type = 'button' AND permission = 'system:role:add'           AND deleted_at = 0;
UPDATE menus SET name = '编辑角色' WHERE type = 'button' AND permission = 'system:role:edit'          AND deleted_at = 0;
UPDATE menus SET name = '删除角色' WHERE type = 'button' AND permission = 'system:role:delete'        AND deleted_at = 0;
UPDATE menus SET name = '分配菜单' WHERE type = 'button' AND permission = 'system:role:assign-menu'   AND deleted_at = 0;
UPDATE menus SET name = '新增菜单' WHERE type = 'button' AND permission = 'system:menu:add'           AND deleted_at = 0;
UPDATE menus SET name = '编辑菜单' WHERE type = 'button' AND permission = 'system:menu:edit'          AND deleted_at = 0;
UPDATE menus SET name = '删除菜单' WHERE type = 'button' AND permission = 'system:menu:delete'        AND deleted_at = 0;
UPDATE menus SET name = '新增部门' WHERE type = 'button' AND permission = 'system:dept:add'           AND deleted_at = 0;
UPDATE menus SET name = '编辑部门' WHERE type = 'button' AND permission = 'system:dept:edit'          AND deleted_at = 0;
UPDATE menus SET name = '删除部门' WHERE type = 'button' AND permission = 'system:dept:delete'        AND deleted_at = 0;
