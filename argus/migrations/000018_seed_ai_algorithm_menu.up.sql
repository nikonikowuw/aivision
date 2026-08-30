-- 000018_seed_ai_algorithm_menu.up.sql
-- 幂等写入 AI 算法(AI)下算法包管理(AiAlgorithm)菜单与权限按钮

DO $$
DECLARE
    v_super_role_id BIGINT;
    v_ai_id BIGINT;
    v_algo_menu_id BIGINT;
    v_btn_id BIGINT;
    r_btn RECORD;
BEGIN
    -- 1. 获取 super 角色
    SELECT id INTO v_super_role_id FROM roles WHERE code = 'super' AND deleted_at = 0 LIMIT 1;

    -- 2. AI 算法 (AI) 目录 (catalog)
    SELECT id INTO v_ai_id FROM menus WHERE parent_id = 0 AND name = 'AI' LIMIT 1;
    IF v_ai_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            0, 'catalog', 'AI', 'routes.ai.ai', '/ai', 'BasicLayout', 'ant-design:robot-outlined', 4, 1,
            '', FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_ai_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_ai_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 3. 算法包管理 (AiAlgorithm) 页面
    SELECT id INTO v_algo_menu_id FROM menus WHERE parent_id = v_ai_id AND name = 'AiAlgorithm' LIMIT 1;
    IF v_algo_menu_id IS NULL THEN
        INSERT INTO menus (
            parent_id, type, name, title, path, component, icon, sort, status,
            permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
        ) VALUES (
            v_ai_id, 'menu', 'AiAlgorithm', 'routes.ai.algorithm', '/ai/algorithm', '/ai/algorithm/index', 'ant-design:appstore-outlined', 1, 1,
            'ai:algorithm', FALSE, TRUE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
        ) RETURNING id INTO v_algo_menu_id;
    END IF;
    IF v_super_role_id IS NOT NULL THEN
        INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_algo_menu_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
    END IF;

    -- 4. 按钮权限
    FOR r_btn IN SELECT * FROM (VALUES
        ('ai.algorithm.upload',    'ai:algorithm:upload',    1),
        ('ai.algorithm.activate',  'ai:algorithm:activate',  2),
        ('ai.algorithm.uninstall', 'ai:algorithm:uninstall', 3)
    ) AS t(name, perm, sort_order)
    LOOP
        v_btn_id := NULL;
        SELECT id INTO v_btn_id
        FROM menus
        WHERE parent_id = v_algo_menu_id AND permission = r_btn.perm
        LIMIT 1;

        IF v_btn_id IS NULL THEN
            INSERT INTO menus (
                parent_id, type, name, title, path, component, icon, sort, status,
                permission, affix, keep_alive, home_path, created_at, updated_at, deleted_at
            ) VALUES (
                v_algo_menu_id, 'button', r_btn.name, r_btn.name, '', '', '', r_btn.sort_order, 1,
                r_btn.perm, FALSE, FALSE, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0
            ) RETURNING id INTO v_btn_id;
        END IF;

        IF v_super_role_id IS NOT NULL THEN
            INSERT INTO role_menus (role_id, menu_id) VALUES (v_super_role_id, v_btn_id) ON CONFLICT (role_id, menu_id) DO NOTHING;
        END IF;
    END LOOP;
END $$;
