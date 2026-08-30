-- 000005_seed_system_rbac.up.sql
-- 幂等写入系统默认角色、初始菜单树与超级管理员绑定（不创建管理员用户）

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_dept_id BIGINT;
    v_system_id BIGINT;
    v_user_id BIGINT;
    v_role_id BIGINT;
    v_menu_id BIGINT;
    v_dept_menu_id BIGINT;
    v_log_id BIGINT;
    v_dashboard_id BIGINT;
    v_dash_analytics_id BIGINT;
    v_btn_id BIGINT;
    
    r_btn RECORD;
BEGIN
    -- 1. super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;
    IF v_super_role_id IS NULL THEN
        INSERT INTO roles (name, code, status, sort, remark, created_at, updated_at, deleted_at)
        VALUES ('超级管理员', 'super', 1, 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0)
        RETURNING id INTO v_super_role_id;
    END IF;

    -- 2. 演示部门
    SELECT id INTO v_dept_id FROM departments WHERE name = '演示部门' AND deleted_at = 0 LIMIT 1;
    IF v_dept_id IS NULL THEN
        INSERT INTO departments (parent_id, name, sort, leader, phone, status, created_at, updated_at, deleted_at)
        VALUES (0, '演示部门', 0, '', '', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0)
        RETURNING id INTO v_dept_id;
    END IF;

    -- 3. System 目录 (catalog)
    SELECT id INTO v_system_id FROM menus WHERE parent_id = 0 AND name = 'System' LIMIT 1;
    IF v_system_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'System', 'routes.system.system', '/system', 'BasicLayout', 'ant-design:setting-outlined', 1, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_system_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_system_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- 3.1 User 菜单
    SELECT id INTO v_user_id FROM menus WHERE parent_id = v_system_id AND name = 'User' LIMIT 1;
    IF v_user_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_system_id, 'menu', 'User', 'routes.system.user', '/system/user', '/system/user/index', 'ant-design:user-outlined', 1, 1,
            'system:user', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_user_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_user_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- User 按钮
    FOR r_btn IN SELECT * FROM (VALUES
        ('新增用户', 'system:user:add', 1),
        ('编辑用户', 'system:user:edit', 2),
        ('删除用户', 'system:user:delete', 3),
        ('重置密码', 'system:user:reset-password', 4),
        ('分配角色', 'system:user:assign-role', 5),
        ('启停用',   'system:user:status', 6)
    ) AS t(name, perm, sort_order)
    LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_user_id AND name = r_btn.name LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_user_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END LOOP;

    -- 3.2 Role 菜单
    SELECT id INTO v_role_id FROM menus WHERE parent_id = v_system_id AND name = 'Role' LIMIT 1;
    IF v_role_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_system_id, 'menu', 'Role', 'routes.system.role', '/system/role', '/system/role/index', 'ant-design:team-outlined', 2, 1,
            'system:role', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_role_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_role_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- Role 按钮
    FOR r_btn IN SELECT * FROM (VALUES
        ('新增角色', 'system:role:add', 1),
        ('编辑角色', 'system:role:edit', 2),
        ('删除角色', 'system:role:delete', 3),
        ('分配菜单', 'system:role:assign-menu', 4)
    ) AS t(name, perm, sort_order)
    LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_role_id AND name = r_btn.name LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_role_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END LOOP;

    -- 3.3 Menu 菜单
    SELECT id INTO v_menu_id FROM menus WHERE parent_id = v_system_id AND name = 'Menu' LIMIT 1;
    IF v_menu_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_system_id, 'menu', 'Menu', 'routes.system.menu', '/system/menu', '/system/menu/index', 'ant-design:menu-outlined', 3, 1,
            'system:menu', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_menu_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_menu_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- Menu 按钮
    FOR r_btn IN SELECT * FROM (VALUES
        ('新增菜单', 'system:menu:add', 1),
        ('编辑菜单', 'system:menu:edit', 2),
        ('删除菜单', 'system:menu:delete', 3)
    ) AS t(name, perm, sort_order)
    LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_menu_id AND name = r_btn.name LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_menu_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END LOOP;

    -- 3.4 Dept 菜单
    SELECT id INTO v_dept_menu_id FROM menus WHERE parent_id = v_system_id AND name = 'Dept' LIMIT 1;
    IF v_dept_menu_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_system_id, 'menu', 'Dept', 'routes.system.dept', '/system/dept', '/system/dept/index', 'ant-design:apartment-outlined', 4, 1,
            'system:dept', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_dept_menu_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_dept_menu_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- Dept 按钮
    FOR r_btn IN SELECT * FROM (VALUES
        ('新增部门', 'system:dept:add', 1),
        ('编辑部门', 'system:dept:edit', 2),
        ('删除部门', 'system:dept:delete', 3)
    ) AS t(name, perm, sort_order)
    LOOP
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_dept_menu_id AND name = r_btn.name LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_dept_menu_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END LOOP;

    -- 3.5 Log 菜单
    SELECT id INTO v_log_id FROM menus WHERE parent_id = v_system_id AND name = 'Log' LIMIT 1;
    IF v_log_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_system_id, 'menu', 'Log', 'routes.system.log', '/system/log', '/system/log/index', 'ant-design:file-text-outlined', 5, 1,
            'system:log', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_log_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_log_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    -- 4. Dashboard 目录 + 视图
    SELECT id INTO v_dashboard_id FROM menus WHERE parent_id = 0 AND name = 'Dashboard' LIMIT 1;
    IF v_dashboard_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'Dashboard', 'routes.dashboard.title', '/dashboard', 'BasicLayout', 'ant-design:home-outlined', 2, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_dashboard_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_dashboard_id) ON CONFLICT (role_id, menu_id) DO NOTHING;

    SELECT id INTO v_dash_analytics_id FROM menus WHERE parent_id = v_dashboard_id AND name = 'dashboard' LIMIT 1;
    IF v_dash_analytics_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_dashboard_id, 'menu', 'dashboard', 'routes.dashboard.analytics', '/dashboard', '/dashboard/analytics/index', 'ant-design:dashboard-outlined', 1, 1,
            '', TRUE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_dash_analytics_id;
    END IF;
    INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_dash_analytics_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
END $$;
