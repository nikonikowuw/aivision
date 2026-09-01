-- 000030_add_persons_primary_face_id.up.sql
-- 为 persons 表添加 primary_face_id 字段，用于标记主图/封面图样本

ALTER TABLE persons ADD COLUMN primary_face_id VARCHAR(64) NOT NULL DEFAULT '';

-- 为存量已有人脸样本的人员自动将第一张样本设为主图
UPDATE persons
SET primary_face_id = (
    SELECT face_id FROM person_faces
    WHERE person_faces.person_id = persons.person_id AND person_faces.deleted_at = 0
    ORDER BY person_faces.id DESC
    LIMIT 1
)
WHERE persons.deleted_at = 0 AND persons.primary_face_id = '' AND EXISTS (
    SELECT 1 FROM person_faces
    WHERE person_faces.person_id = persons.person_id AND person_faces.deleted_at = 0
);
