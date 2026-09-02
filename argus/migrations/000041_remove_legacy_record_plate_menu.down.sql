-- 000041_remove_legacy_record_plate_menu.down.sql
-- 回滚时恢复旧菜单可见性，并恢复 super 角色对该子树的关联。

DO $$
DECLARE
    v_record_id BIGINT;
    v_super_role_id BIGINT;
BEGIN
    SELECT id INTO v_record_id
    FROM menus
    WHERE parent_id = 0 AND name = 'Record'
    LIMIT 1;

    UPDATE menus
    SET deleted_at = 0,
        updated_at = CURRENT_TIMESTAMP
    WHERE parent_id = v_record_id
      AND name = 'RecordPlate';

    WITH RECURSIVE legacy_menu AS (
        SELECT id
        FROM menus
        WHERE name = 'RecordPlate'
          AND parent_id = v_record_id
        UNION ALL
        SELECT child.id
        FROM menus child
        JOIN legacy_menu parent ON child.parent_id = parent.id
    )
    UPDATE menus
    SET deleted_at = 0,
        updated_at = CURRENT_TIMESTAMP
    WHERE id IN (SELECT id FROM legacy_menu);

    SELECT id INTO v_super_role_id
    FROM roles
    WHERE code = 'super' AND deleted_at = 0
    LIMIT 1;

    IF v_super_role_id IS NOT NULL THEN
        WITH RECURSIVE legacy_menu AS (
            SELECT id
            FROM menus
            WHERE name = 'RecordPlate'
              AND parent_id = v_record_id
            UNION ALL
            SELECT child.id
            FROM menus child
            JOIN legacy_menu parent ON child.parent_id = parent.id
        )
        INSERT INTO role_menus (role_id, menu_id)
        SELECT v_super_role_id, id FROM legacy_menu
        ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;
END $$;
