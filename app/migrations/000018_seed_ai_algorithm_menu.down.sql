-- 000018_seed_ai_algorithm_menu.down.sql
DO $$
DECLARE
    v_ai_id BIGINT;
    v_algo_menu_id BIGINT;
BEGIN
    SELECT id INTO v_algo_menu_id FROM menus WHERE name = 'AiAlgorithm' LIMIT 1;
    IF v_algo_menu_id IS NOT NULL THEN
        DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE parent_id = v_algo_menu_id);
        DELETE FROM menus WHERE parent_id = v_algo_menu_id;
        DELETE FROM role_menus WHERE menu_id = v_algo_menu_id;
        DELETE FROM menus WHERE id = v_algo_menu_id;
    END IF;

    SELECT id INTO v_ai_id FROM menus WHERE name = 'AI' AND parent_id = 0 LIMIT 1;
    IF v_ai_id IS NOT NULL THEN
        -- 仅当 AI 目录下已无其他子菜单时才级联删除 AI 目录
        IF NOT EXISTS (SELECT 1 FROM menus WHERE parent_id = v_ai_id) THEN
            DELETE FROM role_menus WHERE menu_id = v_ai_id;
            DELETE FROM menus WHERE id = v_ai_id;
        END IF;
    END IF;
END $$;
