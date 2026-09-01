-- 000029_seed_person_face_permission.down.sql
DELETE FROM role_menus WHERE menu_id IN (
    SELECT id FROM menus WHERE permission = 'resource:person:face:manage'
);
DELETE FROM menus WHERE permission = 'resource:person:face:manage';
