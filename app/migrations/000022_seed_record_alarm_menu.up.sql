-- 000022_seed_record_alarm_menu.up.sql
-- 幂等写入智能记录(Record)目录与告警记录(RecordAlarm)菜单及权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_record_id BIGINT;
    v_alarm_menu_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. 智能记录 (Record) 目录 (catalog)
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

    -- 3. 告警记录 (RecordAlarm) 页面
    SELECT id INTO v_alarm_menu_id FROM menus WHERE parent_id = v_record_id AND name = 'RecordAlarm' LIMIT 1;
    IF v_alarm_menu_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_record_id, 'menu', 'RecordAlarm', 'routes.record.alarm', '/record/alarm', '/record/alarm/index', 'ant-design:alert-outlined', 1, 1,
            'record:alarm', FALSE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_alarm_menu_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_alarm_menu_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('record.alarm.query',  'record:alarm:query',  1),
        ('record.alarm.export', 'record:alarm:export', 2)
    ) AS t(name, perm, sort_order)
    LOOP
        v_btn_id := NULL;
        SELECT id INTO v_btn_id
        FROM menus
        WHERE parent_id = v_alarm_menu_id AND permission = r_btn.perm
        LIMIT 1;

        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_alarm_menu_id, 'button', r_btn.name, r_btn.name, '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;
END $$;
