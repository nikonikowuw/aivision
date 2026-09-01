-- 000028_add_person_faces.up.sql
-- 人员人脸样本表：person_faces
-- 遵循规范：无物理外键，毫秒软删除，独立 SHA-256 去重与 Face ID 唯一索引

CREATE TABLE person_faces (
    id                BIGSERIAL    PRIMARY KEY,
    person_id         VARCHAR(64)  NOT NULL,
    face_id           VARCHAR(64)  NOT NULL,
    algorithm_id      VARCHAR(64)  NOT NULL DEFAULT '',
    algorithm_version VARCHAR(64)  NOT NULL DEFAULT '',
    embedding         BYTEA        NOT NULL,
    quality_score     REAL         NOT NULL DEFAULT 0,
    detection_score   REAL         NOT NULL DEFAULT 0,
    bounding_box      VARCHAR(255) NOT NULL DEFAULT '',
    raw_image_key     VARCHAR(255) NOT NULL,
    raw_image_sha256  VARCHAR(64)  NOT NULL,
    raw_image_size    BIGINT       NOT NULL,
    raw_image_mime    VARCHAR(32)  NOT NULL,
    aligned_face_key  VARCHAR(255) NOT NULL,
    aligned_face_size BIGINT       NOT NULL,
    aligned_face_mime VARCHAR(32)  NOT NULL,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_person_faces_face_id ON person_faces (face_id, deleted_at);
CREATE UNIQUE INDEX uk_person_faces_raw_sha256 ON person_faces (raw_image_sha256, deleted_at);
CREATE INDEX idx_person_faces_person_id ON person_faces (person_id, deleted_at);
