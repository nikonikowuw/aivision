-- 000039_seed_generic_capture_menu.up.sql
-- 幂等维护通用抓拍记录 (RecordCapture) 菜单及权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_record_id BIGINT;
    v_capture_menu_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    SELECT id INTO v_record_id FROM menus WHERE parent_id = 0 AND name = 'Record' LIMIT 1;
    IF v_record_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'Record', 'routes.record.record', '/record', 'BasicLayout', 'ant-design:history-outlined', 5, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_record_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_record_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    SELECT id INTO v_capture_menu_id FROM menus WHERE parent_id = v_record_id AND name = 'RecordCapture' LIMIT 1;
    IF v_capture_menu_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_record_id, 'menu', 'RecordCapture', 'routes.record.capture', '/record/capture', '/record/capture/index', 'ant-design:camera-outlined', 4, 1,
            'record:capture', FALSE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_capture_menu_id;
    ELSE
        UPDATE menus SET
            title = 'routes.record.capture',
            path = '/record/capture',
            component = '/record/capture/index',
            icon = 'ant-design:camera-outlined',
            permission = 'record:capture',
            updated_at = CURRENT_TIMESTAMP
        WHERE id = v_capture_menu_id;
    END IF;

    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_capture_menu_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    FOR r_btn IN SELECT * FROM (VALUES
        ('record.capture.query',  'record:capture:query',  1),
        ('record.capture.export', 'record:capture:export', 2)
    ) AS t(name, perm, sort_order)
    LOOP
        v_btn_id := NULL;
        SELECT id INTO v_btn_id
        FROM menus
        WHERE parent_id = v_capture_menu_id AND permission = r_btn.perm
        LIMIT 1;

        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_capture_menu_id, 'button', r_btn.name, r_btn.name, '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;
END $$;
