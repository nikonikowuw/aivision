-- 000041_remove_legacy_record_plate_menu.up.sql
-- 仅保留抓拍、识别、告警三个记录页面；旧 RecordPlate 菜单软删除并解除角色绑定。

DO $$
DECLARE
    v_deleted_at BIGINT := FLOOR(EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT;
BEGIN
    WITH RECURSIVE legacy_menu AS (
        SELECT id
        FROM menus
        WHERE name = 'RecordPlate'
          AND parent_id IN (SELECT id FROM menus WHERE name = 'Record' AND parent_id = 0)
        UNION ALL
        SELECT child.id
        FROM menus child
        JOIN legacy_menu parent ON child.parent_id = parent.id
    )
    DELETE FROM role_menus
    WHERE menu_id IN (SELECT id FROM legacy_menu);

    WITH RECURSIVE legacy_menu AS (
        SELECT id
        FROM menus
        WHERE name = 'RecordPlate'
          AND parent_id IN (SELECT id FROM menus WHERE name = 'Record' AND parent_id = 0)
        UNION ALL
        SELECT child.id
        FROM menus child
        JOIN legacy_menu parent ON child.parent_id = parent.id
    )
    UPDATE menus
    SET deleted_at = v_deleted_at,
        updated_at = CURRENT_TIMESTAMP
    WHERE id IN (SELECT id FROM legacy_menu)
      AND deleted_at = 0;
END $$;
