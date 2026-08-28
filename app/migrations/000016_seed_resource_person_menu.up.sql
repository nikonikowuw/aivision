-- 000016_seed_resource_person_menu.up.sql
-- 幂等写入资源管理(Resource)下人员管理(ResourcePerson)菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_resource_id BIGINT;
    v_person_id BIGINT;
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

    -- 3. 人员 (ResourcePerson) 页面
    -- 兼容早期未提交版本写入的 Person 名称，并统一修正为路由契约名称。
    SELECT id INTO v_person_id
    FROM menus
    WHERE parent_id = v_resource_id AND name IN ('ResourcePerson', 'Person')
    ORDER BY CASE WHEN name = 'ResourcePerson' THEN 0 ELSE 1 END
    LIMIT 1;
    IF v_person_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_resource_id, 'menu', 'ResourcePerson', 'routes.resource.person', '/resource/person', '/resource/person/index', 'ant-design:idcard-outlined', 2, 1,
            'resource:person', FALSE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_person_id;
    ELSE
        UPDATE menus SET name = 'ResourcePerson' WHERE id = v_person_id AND name = 'Person';
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_person_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('resource.person.add',    'resource:person:add',    1),
        ('resource.person.edit',   'resource:person:edit',   2),
        ('resource.person.delete', 'resource:person:delete', 3)
    ) AS t(name, perm, sort_order)
    LOOP
        -- menus 没有通用唯一键，按父菜单和权限码查找后再插入，避免重复 migration 重复按钮。
        v_btn_id := NULL;
        SELECT id INTO v_btn_id
        FROM menus
        WHERE parent_id = v_person_id AND permission = r_btn.perm
        LIMIT 1;

        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_person_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id)
            ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;

END $$;
