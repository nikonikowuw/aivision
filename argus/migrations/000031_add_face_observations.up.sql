-- 000031_add_face_observations.up.sql
-- 人脸抓拍识别记录模块主表：face_observations
-- 遵循设计原则：单目标轨迹更优覆盖单记录，包含全景大图与人脸特写图相对路径

CREATE TABLE face_observations (
    id                     BIGSERIAL    PRIMARY KEY,
    event_id               VARCHAR(200) NOT NULL,
    instance_id            VARCHAR(64)  NOT NULL DEFAULT '',
    camera_id              VARCHAR(64)  NOT NULL,
    camera_name            VARCHAR(128) NOT NULL DEFAULT '',
    algorithm_id           VARCHAR(64)  NOT NULL DEFAULT '',
    algorithm_version      VARCHAR(32)  NOT NULL DEFAULT '',
    track_id               BIGINT       NOT NULL DEFAULT 0,
    face_id                VARCHAR(64)  NOT NULL DEFAULT '',
    person_id              VARCHAR(64)  NOT NULL DEFAULT '',
    person_name            VARCHAR(128) NOT NULL DEFAULT '',
    similarity             REAL         NOT NULL DEFAULT 0,
    bbox_json              JSONB        NOT NULL DEFAULT '{}'::jsonb,
    time_synced            BOOLEAN      NOT NULL DEFAULT FALSE,
    image_id               VARCHAR(200) NOT NULL DEFAULT '',
    image_rel_path         VARCHAR(255) NOT NULL DEFAULT '',
    face_image_id          VARCHAR(200) NOT NULL DEFAULT '',
    face_image_rel_path    VARCHAR(255) NOT NULL DEFAULT '',
    observed_at            TIMESTAMPTZ  NOT NULL,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at             BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_face_observations_event_id ON face_observations (event_id, deleted_at);
CREATE INDEX idx_face_observations_observed_at ON face_observations (observed_at DESC);
CREATE INDEX idx_face_observations_person_id ON face_observations (person_id);
CREATE INDEX idx_face_observations_camera_id ON face_observations (camera_id);
CREATE INDEX idx_face_observations_image_id ON face_observations (image_id);
CREATE INDEX idx_face_observations_face_image_id ON face_observations (face_image_id);
