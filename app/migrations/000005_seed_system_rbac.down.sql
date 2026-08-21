-- 000005_seed_system_rbac.down.sql
-- 清理内置演示部门、菜单和 super 角色绑定

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_dept_id BIGINT;
BEGIN
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' LIMIT 1;
    IF v_super_role_id IS NOT NULL THEN
        DELETE FROM role_menus WHERE role_id = v_super_role_id;
    END IF;

    -- 清理标准内置菜单
    DELETE FROM menus WHERE name IN ('System', 'Dashboard');

    -- 清理演示部门
    SELECT id INTO v_dept_id FROM departments WHERE name = '演示部门' LIMIT 1;
    IF v_dept_id IS NOT NULL THEN
        DELETE FROM departments WHERE id = v_dept_id;
    END IF;
END $$;
