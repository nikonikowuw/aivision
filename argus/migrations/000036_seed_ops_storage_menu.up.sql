-- 000036_seed_ops_storage_menu.up.sql
-- 幂等写入存储管理 (Storage) 菜单、按钮权限及默认保留策略配置

-- 1. 初始化存储策略默认配置 (key: 'system:storage:retention')
INSERT INTO system_configs (key, value, remark)
VALUES (
    'system:storage:retention',
    '{"retentionDays": 30, "highWatermarkPercent": 85, "lowWatermarkPercent": 70, "checkIntervalSeconds": 600, "autoCleanupEnabled": true}'::jsonb,
    '存储保留与清理策略配置'
)
ON CONFLICT (key) DO NOTHING;

-- 2. 幂等创建 Ops 下的 Storage 菜单与按钮权限
DO $$
DECLARE
    v_super_role_id BIGINT;
    v_ops_id BIGINT;
    v_storage_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 运维管理 (Ops) 目录
    SELECT id INTO v_ops_id FROM menus WHERE parent_id = 0 AND name = 'Ops' LIMIT 1;
    IF v_ops_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'Ops', 'routes.ops.ops', '/ops', 'BasicLayout', 'ant-design:tool-outlined', 2, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_ops_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_ops_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 存储管理 (Storage) 菜单
    SELECT id INTO v_storage_id FROM menus WHERE parent_id = v_ops_id AND name = 'Storage' LIMIT 1;
    IF v_storage_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_ops_id, 'menu', 'Storage', 'routes.ops.storage', '/ops/storage', '/ops/storage/index', 'ant-design:hdd-outlined', 3, 1,
            'ops:storage', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_storage_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_storage_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 按钮权限 (ops:storage:read, ops:storage:edit)
    FOR r_btn IN (
        SELECT 'ops.storage.read' AS name, 'ops.storage.read' AS title, 'ops:storage:read' AS perm, 1 AS sort
        UNION ALL
        SELECT 'ops.storage.edit' AS name, 'ops.storage.edit' AS title, 'ops:storage:edit' AS perm, 2 AS sort
    ) LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_storage_id AND permission = r_btn.perm LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_storage_id, 'button', r_btn.name, r_btn.title, '', '', '', r_btn.sort, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        ELSE
            UPDATE menus SET name = r_btn.name, title = r_btn.title, sort = r_btn.sort WHERE id = v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;
END $$;
