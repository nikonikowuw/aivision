-- 000022_seed_record_alarm_menu.down.sql
-- 回滚：删除智能记录(Record)与告警记录(RecordAlarm)菜单及角色关联

DO $$
DECLARE
    v_record_id BIGINT;
    v_alarm_id BIGINT;
BEGIN
    SELECT id INTO v_alarm_id FROM menus WHERE path = '/record/alarm' AND type = 'menu' LIMIT 1;
    IF v_alarm_id IS NOT NULL THEN
        DELETE FROM role_menus WHERE menu_id IN (
            SELECT id FROM menus WHERE parent_id = v_alarm_id
        );
        DELETE FROM role_menus WHERE menu_id = v_alarm_id;
        DELETE FROM menus WHERE parent_id = v_alarm_id;
        DELETE FROM menus WHERE id = v_alarm_id;
    END IF;

    SELECT id INTO v_record_id FROM menus WHERE name = 'Record' AND parent_id = 0 LIMIT 1;
    IF v_record_id IS NOT NULL THEN
        -- 如果目录下无其他子菜单，则清理该 catalog
        IF NOT EXISTS (SELECT 1 FROM menus WHERE parent_id = v_record_id) THEN
            DELETE FROM role_menus WHERE menu_id = v_record_id;
            DELETE FROM menus WHERE id = v_record_id;
        END IF;
    END IF;
END $$;
