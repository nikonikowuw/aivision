-- 000008_seed_ops_time.down.sql
-- 回滚 Ops catalog 与 Time 菜单及相关权限

DO $$
DECLARE
    v_ops_id BIGINT;
    v_time_id BIGINT;
BEGIN
    SELECT id INTO v_ops_id FROM menus WHERE parent_id = 0 AND name = 'Ops' LIMIT 1;
    IF v_ops_id IS NOT NULL THEN
        SELECT id INTO v_time_id FROM menus WHERE parent_id = v_ops_id AND name = 'Time' LIMIT 1;
        IF v_time_id IS NOT NULL THEN
            -- 删除按钮权限关联及菜单
            DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE parent_id = v_time_id);
            DELETE FROM menus WHERE parent_id = v_time_id;

            -- 删除 Time 菜单关联及菜单
            DELETE FROM role_menus WHERE menu_id = v_time_id;
            DELETE FROM menus WHERE id = v_time_id;
        END IF;

        -- 删除 Ops 目录关联及目录（如果已无其他子菜单）
        IF NOT EXISTS (SELECT 1 FROM menus WHERE parent_id = v_ops_id) THEN
            DELETE FROM role_menus WHERE menu_id = v_ops_id;
            DELETE FROM menus WHERE id = v_ops_id;
        END IF;
    END IF;
END $$;
