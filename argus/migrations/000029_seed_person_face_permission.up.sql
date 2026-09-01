-- 000029_seed_person_face_permission.up.sql
-- 幂等写入人员人脸样本管理按钮权限 (resource:person:face:manage)

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_person_id BIGINT;
    v_btn_id BIGINT;
BEGIN
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;
    SELECT id INTO v_person_id FROM menus WHERE name = 'ResourcePerson' AND deleted_at = 0 LIMIT 1;

    IF v_person_id IS NOT NULL THEN
        SELECT id INTO v_btn_id FROM menus WHERE parent_id = v_person_id AND permission = 'resource:person:face:manage' LIMIT 1;
        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_person_id, 'button', 'resource.person.faceManage', '', '', '', '', 4, 1,
                'resource:person:face:manage', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL AND v_btn_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END IF;
END $$;
