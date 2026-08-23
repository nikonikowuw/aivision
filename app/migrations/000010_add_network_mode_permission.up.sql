-- 000010_add_network_mode_permission.up.sql
-- 幂等在 Network 菜单下新增「切换网络工作模式」按钮权限（ops:network:mode）

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_network_id BIGINT;
BEGIN
    -- 1. 获取 super 角色与 Network 菜单（000009 已创建；此处若缺失则静默跳过）
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;
    SELECT id INTO v_network_id FROM menus WHERE type = 'menu' AND name = 'Network' AND deleted_at = 0 LIMIT 1;

    IF v_network_id IS NOT NULL THEN
        -- 2. 按钮权限节点（幂等：已存在则跳过）
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_network_id, 'button', 'ops.network.mode', '', '', '', '', 5, 1,
            'ops:network:mode', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) ON CONFLICT DO NOTHING;

        -- 3. 绑定给 super 角色
        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id)
            SELECT v_super_role_id, id FROM menus
            WHERE parent_id = v_network_id AND permission = 'ops:network:mode' AND deleted_at = 0
            ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END IF;
END $$;
