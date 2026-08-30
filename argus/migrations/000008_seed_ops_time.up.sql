-- 000008_seed_ops_time.up.sql
-- 幂等创建 Ops catalog 与 Time (时间管理) 菜单及按钮权限，并绑定给 super 角色

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_ops_id BIGINT;
    v_time_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. Ops 目录 (catalog)
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

    -- 2.1 Time (时间管理) 菜单
    SELECT id INTO v_time_id FROM menus WHERE parent_id = v_ops_id AND name = 'Time' LIMIT 1;
    IF v_time_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_ops_id, 'menu', 'Time', 'routes.ops.time', '/ops/time', '/ops/time/index', 'ant-design:field-time-outlined', 1, 1,
            'ops:time', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_time_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_time_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 2.2 按钮权限 (ops:time:read, ops:time:edit)
    FOR r_btn IN (
        SELECT 'ops.time.read' AS name, 'ops.time.read' AS title, 'ops:time:read' AS perm, 1 AS sort
        UNION ALL
        SELECT 'ops.time.edit' AS name, 'ops.time.edit' AS title, 'ops:time:edit' AS perm, 2 AS sort
    ) LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_time_id AND permission = r_btn.perm LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_time_id, 'button', r_btn.name, r_btn.title, '', '', '', r_btn.sort, 1,
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
