-- 000020_seed_resource_task_menu.down.sql
-- 回滚：删除任务管理(ResourceTask)菜单及按钮与角色关联

DO $$
DECLARE
    v_task_id BIGINT;
BEGIN
    SELECT id INTO v_task_id FROM menus WHERE path = '/resource/task' AND type = 'menu' LIMIT 1;
    IF v_task_id IS NOT NULL THEN
        -- 删除角色与任务按钮/页面的关联
        DELETE FROM role_menus WHERE menu_id IN (
            SELECT id FROM menus WHERE parent_id = v_task_id
        );
        DELETE FROM role_menus WHERE menu_id = v_task_id;

        -- 删除按钮与菜单自身
        DELETE FROM menus WHERE parent_id = v_task_id;
        DELETE FROM menus WHERE id = v_task_id;
    END IF;
END $$;
