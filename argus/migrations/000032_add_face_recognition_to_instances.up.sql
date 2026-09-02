-- 000032_add_face_recognition_to_instances.up.sql
ALTER TABLE algorithm_instances ADD COLUMN IF NOT EXISTS face_recognition_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 人脸底库版本计数器：单行 id=1，只增不减。
-- 业务事务内 UPDATE ... RETURNING 取新值，独立于 desired_state_revision。
CREATE TABLE IF NOT EXISTS face_gallery_revision (
    id       SMALLINT PRIMARY KEY DEFAULT 1,
    revision BIGINT   NOT NULL DEFAULT 0,
    CONSTRAINT ck_face_gallery_revision_singleton CHECK (id = 1)
);
INSERT INTO face_gallery_revision (id, revision) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;
