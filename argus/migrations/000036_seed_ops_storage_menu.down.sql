-- 000036_seed_ops_storage_menu.down.sql

DELETE FROM role_menus WHERE menu_id IN (
    SELECT id FROM menus WHERE permission IN ('ops:storage:read', 'ops:storage:edit', 'ops:storage')
);

DELETE FROM menus WHERE permission IN ('ops:storage:read', 'ops:storage:edit', 'ops:storage');

DELETE FROM system_configs WHERE key = 'system:storage:retention';
