-- 000017_add_algorithms.up.sql
-- 算法与算法版本表（算法包管理模块）
-- 遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（BaseModel）。

CREATE TABLE algorithms (
    id             BIGSERIAL PRIMARY KEY,
    algorithm_id   VARCHAR(64) NOT NULL,
    name           VARCHAR(128) NOT NULL,
    algorithm_type VARCHAR(32) NOT NULL,
    alarm_type_id  VARCHAR(64) NOT NULL DEFAULT '',
    active_version VARCHAR(32) NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at     BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_algorithms_id ON algorithms (algorithm_id, deleted_at);
CREATE INDEX idx_algorithms_type ON algorithms (deleted_at, algorithm_type);

CREATE TABLE algorithm_versions (
    id                  BIGSERIAL PRIMARY KEY,
    algorithm_id        VARCHAR(64) NOT NULL,
    version             VARCHAR(32) NOT NULL,
    platform_id         VARCHAR(64) NOT NULL,
    min_adapter_version VARCHAR(32) NOT NULL DEFAULT '',
    package_root        VARCHAR(255) NOT NULL DEFAULT '',
    fps_tiers           JSONB NOT NULL DEFAULT '[]'::jsonb,
    config_schema       JSONB NOT NULL DEFAULT '{}'::jsonb,
    manifest_raw        JSONB NOT NULL DEFAULT '{}'::jsonb,
    package_size_bytes  BIGINT NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_algo_versions_algo_ver ON algorithm_versions (algorithm_id, version, deleted_at);
CREATE INDEX idx_algo_versions_algo_id ON algorithm_versions (deleted_at, algorithm_id);
