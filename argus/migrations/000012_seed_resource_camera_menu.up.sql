-- 000012_seed_resource_camera_menu.up.sql
-- 幂等写入资源管理(Resource)目录及摄像头(Camera)菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_resource_id BIGINT;
    v_camera_id BIGINT;
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

    -- 3. 摄像头 (Camera) 页面
    SELECT id INTO v_camera_id FROM menus WHERE parent_id = v_resource_id AND name = 'Camera' LIMIT 1;
    IF v_camera_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_resource_id, 'menu', 'Camera', 'routes.resource.camera', '/resource/camera', '/resource/camera/index', 'ant-design:video-camera-outlined', 1, 1,
            'resource:camera', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_camera_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_camera_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('resource.camera.add',    'resource:camera:add',    1),
        ('resource.camera.edit',   'resource:camera:edit',   2),
        ('resource.camera.delete', 'resource:camera:delete', 3),
        ('resource.camera.probe',  'resource:camera:probe',  4)
    ) AS t(name, perm, sort_order)
    LOOP
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_camera_id, 'button', r_btn.name, '', '', '', '', r_btn.sort_order, 1,
            r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) ON CONFLICT DO NOTHING;
    END LOOP;

    -- 将新添加的按钮权限绑定给 super 角色
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id)
        SELECT v_super_role_id, id FROM menus WHERE parent_id = v_camera_id
        ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

END $$;
