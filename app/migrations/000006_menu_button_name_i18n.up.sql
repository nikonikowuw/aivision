-- 000006_menu_button_name_i18n.up.sql
-- 按钮级菜单 name 由中文展示名迁移为标准 i18n key（决策 17，与操作日志 action 契约一致），
-- 按 permission 码幂等匹配，未匹配的用户自建按钮保持原值。

UPDATE menus SET name = 'system.user.addUser'        WHERE type = 'button' AND permission = 'system:user:add'           AND deleted_at = 0;
UPDATE menus SET name = 'system.user.editUser'       WHERE type = 'button' AND permission = 'system:user:edit'          AND deleted_at = 0;
UPDATE menus SET name = 'system.user.deleteUser'     WHERE type = 'button' AND permission = 'system:user:delete'        AND deleted_at = 0;
UPDATE menus SET name = 'system.user.resetPassword'  WHERE type = 'button' AND permission = 'system:user:reset-password' AND deleted_at = 0;
UPDATE menus SET name = 'system.user.assignRole'     WHERE type = 'button' AND permission = 'system:user:assign-role'    AND deleted_at = 0;
UPDATE menus SET name = 'system.user.status'         WHERE type = 'button' AND permission = 'system:user:status'        AND deleted_at = 0;
UPDATE menus SET name = 'system.role.addRole'        WHERE type = 'button' AND permission = 'system:role:add'           AND deleted_at = 0;
UPDATE menus SET name = 'system.role.editRole'       WHERE type = 'button' AND permission = 'system:role:edit'          AND deleted_at = 0;
UPDATE menus SET name = 'system.role.deleteRole'     WHERE type = 'button' AND permission = 'system:role:delete'        AND deleted_at = 0;
UPDATE menus SET name = 'system.role.assignMenu'     WHERE type = 'button' AND permission = 'system:role:assign-menu'   AND deleted_at = 0;
UPDATE menus SET name = 'system.menu.addMenu'        WHERE type = 'button' AND permission = 'system:menu:add'           AND deleted_at = 0;
UPDATE menus SET name = 'system.menu.editMenu'       WHERE type = 'button' AND permission = 'system:menu:edit'          AND deleted_at = 0;
UPDATE menus SET name = 'system.menu.deleteMenu'     WHERE type = 'button' AND permission = 'system:menu:delete'        AND deleted_at = 0;
UPDATE menus SET name = 'system.dept.addDept'        WHERE type = 'button' AND permission = 'system:dept:add'           AND deleted_at = 0;
UPDATE menus SET name = 'system.dept.editDept'       WHERE type = 'button' AND permission = 'system:dept:edit'          AND deleted_at = 0;
UPDATE menus SET name = 'system.dept.deleteDept'     WHERE type = 'button' AND permission = 'system:dept:delete'        AND deleted_at = 0;
