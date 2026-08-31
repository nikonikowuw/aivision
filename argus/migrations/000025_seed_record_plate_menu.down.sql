-- 000025_seed_record_plate_menu.down.sql
DELETE FROM menus WHERE permission IN ('record:plate', 'record:plate:query', 'record:plate:export');
