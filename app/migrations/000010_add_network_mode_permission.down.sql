-- 000010_add_network_mode_permission.down.sql
-- 回滚：按 permission 删除 ops:network:mode 按钮及其角色关联

DO $$
DECLARE
    v_menu_id BIGINT;
BEGIN
    SELECT id INTO v_menu_id FROM menus WHERE permission = 'ops:network:mode' AND deleted_at = 0 LIMIT 1;
    IF v_menu_id IS NOT NULL THEN
        DELETE FROM role_menus WHERE menu_id = v_menu_id;
        DELETE FROM menus WHERE id = v_menu_id;
    END IF;
END $$;
