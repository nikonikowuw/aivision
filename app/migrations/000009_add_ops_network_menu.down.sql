-- 000007_add_ops_network_menu.down.sql
-- 回滚网络配置菜单及权限

DO $$
DECLARE
    v_ops_id BIGINT;
    v_network_id BIGINT;
BEGIN
    SELECT id INTO v_network_id FROM menus WHERE name = 'Network' AND type = 'menu' LIMIT 1;
    IF v_network_id IS NOT NULL THEN
        -- 删除角色关联
        DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE parent_id = v_network_id OR id = v_network_id);
        -- 删除按钮与页面
        DELETE FROM menus WHERE parent_id = v_network_id;
        DELETE FROM menus WHERE id = v_network_id;
    END IF;

    -- 若 Ops 目录下无其他子菜单，清理 Ops 目录
    SELECT id INTO v_ops_id FROM menus WHERE name = 'Ops' AND type = 'catalog' LIMIT 1;
    IF v_ops_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM menus WHERE parent_id = v_ops_id) THEN
            DELETE FROM role_menus WHERE menu_id = v_ops_id;
            DELETE FROM menus WHERE id = v_ops_id;
        END IF;
    END IF;
END $$;
