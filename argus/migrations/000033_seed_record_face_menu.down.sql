-- 000033_seed_record_face_menu.down.sql
DELETE FROM menus WHERE permission IN ('record:face', 'record:face:query', 'record:face:export');
