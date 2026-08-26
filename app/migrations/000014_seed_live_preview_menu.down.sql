-- 000014_seed_live_preview_menu.down.sql
DO $$
DECLARE
    v_live_id BIGINT;
BEGIN
    SELECT id INTO v_live_id FROM menus WHERE parent_id = 0 AND name = 'LivePreview' LIMIT 1;
    IF v_live_id IS NOT NULL THEN
        DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE parent_id = v_live_id);
        DELETE FROM menus WHERE parent_id = v_live_id;
        DELETE FROM role_menus WHERE menu_id = v_live_id;
        DELETE FROM menus WHERE id = v_live_id;
    END IF;
END $$;
