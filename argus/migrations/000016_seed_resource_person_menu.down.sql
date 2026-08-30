-- 000016_seed_resource_person_menu.down.sql
-- 回滚：删除人员管理(Person)菜单及按钮与角色关联

DO $$
DECLARE
    v_person_id BIGINT;
BEGIN
    SELECT id INTO v_person_id FROM menus WHERE path = '/resource/person' AND type = 'menu' LIMIT 1;
    IF v_person_id IS NOT NULL THEN
        -- 删除角色与人员按钮/页面的关联
        DELETE FROM role_menus WHERE menu_id IN (
            SELECT id FROM menus WHERE parent_id = v_person_id
        );
        DELETE FROM role_menus WHERE menu_id = v_person_id;

        -- 删除按钮与菜单自身
        DELETE FROM menus WHERE parent_id = v_person_id;
        DELETE FROM menus WHERE id = v_person_id;
    END IF;
END $$;
