-- 000019_add_analysis_tasks.up.sql
-- 任务配置模块三张表：analysis_tasks / algorithm_instances / desired_state_revision。
-- 遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（BaseModel，deleted_at=0 表示活跃，
-- 唯一约束必须复合 deleted_at，见 database-guidelines）。

-- 分析任务：与摄像头 1:1，camera_id 即任务标识（不发明独立 task_id，见 design D2）。
CREATE TABLE analysis_tasks (
    id              BIGSERIAL    PRIMARY KEY,
    camera_id       VARCHAR(36)  NOT NULL,
    name            VARCHAR(128) NOT NULL,
    desired_enabled BOOLEAN      NOT NULL DEFAULT FALSE,
    actual_status   SMALLINT     NOT NULL DEFAULT 0,
    status_message  VARCHAR(255) NOT NULL DEFAULT '',
    last_frame_at   TIMESTAMPTZ,
    reported_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      BIGINT       NOT NULL DEFAULT 0
);

-- 复合唯一索引（含 deleted_at）：任务软删后同一 camera_id 可重新建任务（D8）。
CREATE UNIQUE INDEX uk_analysis_tasks_camera_id ON analysis_tasks (camera_id, deleted_at);

-- 算法实例：挂在 camera_id 下（不经 analysis_tasks.id，与 Engine 寻址一致，见 design D2）。
-- 无 algorithm_version 列（D11）：组装 DesiredState 时从 algorithms.active_version 动态填充。
CREATE TABLE algorithm_instances (
    id             BIGSERIAL   PRIMARY KEY,
    instance_id    VARCHAR(36) NOT NULL,
    camera_id      VARCHAR(36) NOT NULL,
    algorithm_id   VARCHAR(64) NOT NULL,
    analysis_fps   INTEGER     NOT NULL DEFAULT 0,
    params_json    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rules_json     JSONB       NOT NULL DEFAULT '[]'::jsonb,
    enabled        BOOLEAN     NOT NULL DEFAULT FALSE,
    actual_status  SMALLINT    NOT NULL DEFAULT 0,
    status_message VARCHAR(255) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at     BIGINT      NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_algorithm_instances_instance_id ON algorithm_instances (instance_id, deleted_at);
CREATE INDEX idx_algorithm_instances_camera_id ON algorithm_instances (deleted_at, camera_id);
CREATE INDEX idx_algorithm_instances_algorithm_id ON algorithm_instances (deleted_at, algorithm_id);

-- 期望状态版本计数器：单行 id=1，只增不减（D4）。
-- 业务事务内 UPDATE ... RETURNING 取新值；删除持有最大 revision 的行会导致版本回退，
-- 因此 revision 必须由本表独立维护，不能取各行业务行的 max()。
CREATE TABLE desired_state_revision (
    id       SMALLINT PRIMARY KEY DEFAULT 1,
    revision BIGINT   NOT NULL DEFAULT 0,
    CONSTRAINT ck_desired_state_revision_singleton CHECK (id = 1)
);
INSERT INTO desired_state_revision (id, revision) VALUES (1, 0);
