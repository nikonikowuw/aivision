-- 000007_add_ops_network_menu.up.sql
-- 幂等写入运维管理(Ops)目录及网络配置(Network)菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_ops_id BIGINT;
    v_network_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. 运维管理 (Ops) 目录 (catalog)
    SELECT id INTO v_ops_id FROM menus WHERE parent_id = 0 AND name = 'Ops' LIMIT 1;
    IF v_ops_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'Ops', 'routes.ops.ops', '/ops', 'BasicLayout', 'ant-design:tool-outlined', 4, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_ops_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_ops_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 3. 网络配置 (Network) 页面
    SELECT id INTO v_network_id FROM menus WHERE parent_id = v_ops_id AND name = 'Network' LIMIT 1;
    IF v_network_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_ops_id, 'menu', 'Network', 'routes.ops.network', '/ops/network', '/ops/network/index', 'ant-design:global-outlined', 1, 1,
            'ops:network', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_network_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_network_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('system.common.edit',  'ops:network:edit', 1),
        ('ops.network.confirm', 'ops:network:confirm', 2),
        ('ops.network.cancel',  'ops:network:cancel', 3),
        ('ops.network.reset',   'ops:network:reset', 4)
    ) AS t(name, perm, sort_order)
    LOOP
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_network_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
            r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) ON CONFLICT DO NOTHING;
    END LOOP;

    -- 将新添加的按钮权限绑定给 super 角色
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id)
        SELECT v_super_role_id, id FROM menus WHERE parent_id = v_network_id
        ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

END $$;
