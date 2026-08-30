-- 000020_seed_resource_task_menu.up.sql
-- 幂等写入资源管理(Resource)下任务管理(ResourceTask)菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_resource_id BIGINT;
    v_task_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. 资源管理 (Resource) 目录 (catalog)
    SELECT id INTO v_resource_id FROM menus WHERE parent_id = 0 AND name = 'Resource' LIMIT 1;
    IF v_resource_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'Resource', 'routes.resource.resource', '/resource', 'BasicLayout', 'ant-design:database-outlined', 3, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_resource_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_resource_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 3. 任务管理 (ResourceTask) 页面
    -- 兼容 Task / ResourceTask 命名，统一修正为 ResourceTask（PRD §7.17.3 / §7.17.5 keep_alive = true）
    SELECT id INTO v_task_id
    FROM menus
    WHERE parent_id = v_resource_id AND name IN ('ResourceTask', 'Task')
    ORDER BY CASE WHEN name = 'ResourceTask' THEN 0 ELSE 1 END
    LIMIT 1;

    IF v_task_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_resource_id, 'menu', 'ResourceTask', 'routes.resource.task', '/resource/task', '/resource/task/index', 'ant-design:profile-outlined', 2, 1,
            'resource:task', FALSE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_task_id;
    ELSE
        UPDATE menus
        SET name = 'ResourceTask',
            title = 'routes.resource.task',
            path = '/resource/task',
            component = '/resource/task/index',
            icon = 'ant-design:profile-outlined',
            sort = 2,
            status = 1,
            permission = 'resource:task',
            keep_alive = TRUE
        WHERE id = v_task_id;
    END IF;

    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_task_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('resource.task.add',    'resource:task:add',    1),
        ('resource.task.edit',   'resource:task:edit',   2),
        ('resource.task.delete', 'resource:task:delete', 3)
    ) AS t(name, perm, sort_order)
    LOOP
        v_btn_id := NULL;
        SELECT id INTO v_btn_id
        FROM menus
        WHERE parent_id = v_task_id AND permission = r_btn.perm
        LIMIT 1;

        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_task_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id)
            ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;

END $$;
