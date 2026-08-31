-- 000024_add_plate_observations.up.sql
-- 车牌抓拍过车记录模块主表：plate_observations
-- 遵循设计原则：单过车识别目标单记录，包含全景大图与车牌特写图相对路径

CREATE TABLE plate_observations (
    id                 BIGSERIAL    PRIMARY KEY,
    event_id           VARCHAR(200) NOT NULL,
    task_id            VARCHAR(64)  NOT NULL DEFAULT '',
    instance_id        VARCHAR(64)  NOT NULL DEFAULT '',
    camera_id          VARCHAR(64)  NOT NULL,
    camera_name        VARCHAR(128) NOT NULL DEFAULT '',
    plate_text         VARCHAR(32)  NOT NULL,
    normalized_text    VARCHAR(32)  NOT NULL,
    plate_color        VARCHAR(16)  NOT NULL DEFAULT '',
    plate_type         VARCHAR(32)  NOT NULL DEFAULT '',
    confidence         REAL         NOT NULL DEFAULT 0,
    ocr_confidence     REAL         NOT NULL DEFAULT 0,
    track_id           BIGINT       NOT NULL DEFAULT 0,
    bbox_json          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    vehicle_bbox_json  JSONB        NOT NULL DEFAULT '{}'::jsonb,
    panorama_image     VARCHAR(255) NOT NULL DEFAULT '',
    plate_image        VARCHAR(255) NOT NULL DEFAULT '',
    observed_at        TIMESTAMPTZ  NOT NULL,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at         BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_plate_observations_event_id ON plate_observations (event_id, deleted_at);
CREATE INDEX idx_plate_observations_observed_at ON plate_observations (observed_at DESC);
CREATE INDEX idx_plate_observations_camera_id ON plate_observations (camera_id);
CREATE INDEX idx_plate_observations_plate_text ON plate_observations (plate_text);
CREATE INDEX idx_plate_observations_normalized_text ON plate_observations (normalized_text);
CREATE INDEX idx_plate_observations_plate_color ON plate_observations (plate_color);
