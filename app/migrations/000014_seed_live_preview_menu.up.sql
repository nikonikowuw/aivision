-- 000014_seed_live_preview_menu.up.sql
-- 幂等写入实时预览 (LivePreview) 顶层菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_live_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. 实时预览 (LivePreview) 顶层菜单 (menu)
    SELECT id INTO v_live_id FROM menus WHERE parent_id = 0 AND name = 'LivePreview' LIMIT 1;
    IF v_live_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'menu', 'LivePreview', 'routes.live.live', '/live', '/live/index', 'ant-design:video-camera-outlined', 1, 1,
            'live:preview', TRUE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_live_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_live_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 3. 按钮权限（live:preview:stream 实时取流权限）
    FOR r_btn IN SELECT * FROM (VALUES
        ('live.preview.stream', 'live:preview:stream', 1)
    ) AS t(name, perm, sort_order)
    LOOP
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_live_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
            r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) ON CONFLICT DO NOTHING;
    END LOOP;

    -- 将新添加的按钮权限绑定给 super 角色
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id)
        SELECT v_super_role_id, id FROM menus WHERE parent_id = v_live_id
        ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

END $$;
