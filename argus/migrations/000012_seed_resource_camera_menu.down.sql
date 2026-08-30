-- 000012_seed_resource_camera_menu.down.sql
-- 回滚：删除摄像头菜单、按钮及其 super 绑定（保留 Resource 目录本身，避免误删其他资源子菜单）

DELETE FROM role_menus
WHERE menu_id IN (
    SELECT id FROM menus WHERE parent_id IN (
        SELECT id FROM menus WHERE type = 'menu' AND name = 'Camera' AND deleted_at = 0
    ) AND type = 'button' AND deleted_at = 0
);

DELETE FROM menus
WHERE type = 'button' AND parent_id IN (
    SELECT id FROM menus WHERE type = 'menu' AND name = 'Camera' AND deleted_at = 0
);

DELETE FROM role_menus
WHERE menu_id IN (
    SELECT id FROM menus WHERE type = 'menu' AND name = 'Camera' AND deleted_at = 0
);

DELETE FROM menus WHERE type = 'menu' AND name = 'Camera' AND deleted_at = 0;
