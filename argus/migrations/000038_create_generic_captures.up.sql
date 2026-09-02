-- 000038_create_generic_captures.up.sql
-- 通用抓拍记录主表：captures
-- 统一承载人脸、人体、机动车/车牌、非机动车等多态感知目标，支持自适应人脸-人体联合抓拍

CREATE TABLE captures (
    id                      BIGSERIAL    PRIMARY KEY,
    event_id                VARCHAR(200) NOT NULL,
    instance_id             VARCHAR(64)  NOT NULL DEFAULT '',
    target_type             VARCHAR(32)  NOT NULL DEFAULT 'generic',
    camera_id               VARCHAR(64)  NOT NULL,
    camera_name             VARCHAR(128) NOT NULL DEFAULT '',
    task_id                 BIGINT       NOT NULL DEFAULT 0,
    algorithm_id            VARCHAR(64)  NOT NULL DEFAULT '',
    algorithm_version       VARCHAR(32)  NOT NULL DEFAULT '',
    track_id                BIGINT       NOT NULL DEFAULT 0,
    confidence              REAL         NOT NULL DEFAULT 0,
    quality_score           REAL         NOT NULL DEFAULT 0,
    bbox_json               JSONB        NOT NULL DEFAULT '{}'::jsonb,
    image_id                VARCHAR(200) NOT NULL DEFAULT '',
    image_rel_path          VARCHAR(255) NOT NULL DEFAULT '',
    crop_image_id           VARCHAR(200) NOT NULL DEFAULT '',
    crop_image_rel_path     VARCHAR(255) NOT NULL DEFAULT '',
    sub_crop_image_id       VARCHAR(200) NOT NULL DEFAULT '',
    sub_crop_image_rel_path VARCHAR(255) NOT NULL DEFAULT '',
    is_recognized           BOOLEAN      NOT NULL DEFAULT FALSE,
    attributes_json         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    captured_at             TIMESTAMPTZ  NOT NULL,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at              BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_captures_event_id ON captures (event_id, deleted_at);
CREATE INDEX idx_captures_captured_at ON captures (captured_at DESC);
CREATE INDEX idx_captures_target_type ON captures (target_type);
CREATE INDEX idx_captures_camera_id ON captures (camera_id);
CREATE INDEX idx_captures_task_id ON captures (task_id);
CREATE INDEX idx_captures_is_recognized ON captures (is_recognized);
