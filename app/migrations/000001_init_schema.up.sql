-- 000001_init_schema.up.sql
-- 初始 8 表 baseline schema (PostgreSQL)

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    username VARCHAR(64) NOT NULL,
    password VARCHAR(255),
    nickname VARCHAR(64),
    email VARCHAR(128),
    phone VARCHAR(32),
    avatar VARCHAR(255),
    dept_id BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 1,
    remark VARCHAR(255)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (username);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);
CREATE INDEX IF NOT EXISTS idx_users_dept_id ON users (dept_id);

CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    name VARCHAR(64) NOT NULL,
    code VARCHAR(64) NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    sort INTEGER NOT NULL DEFAULT 0,
    remark VARCHAR(255)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_code ON roles (code);
CREATE INDEX IF NOT EXISTS idx_roles_deleted_at ON roles (deleted_at);

CREATE TABLE IF NOT EXISTS menus (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    parent_id BIGINT NOT NULL DEFAULT 0,
    type VARCHAR(16) NOT NULL,
    name VARCHAR(64) NOT NULL,
    title VARCHAR(128),
    path VARCHAR(128),
    component VARCHAR(255),
    icon VARCHAR(64),
    sort INTEGER NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 1,
    permission VARCHAR(128),
    affix BOOLEAN NOT NULL DEFAULT FALSE,
    keep_alive BOOLEAN NOT NULL DEFAULT FALSE,
    home_path VARCHAR(128)
);
CREATE INDEX IF NOT EXISTS idx_menus_deleted_at ON menus (deleted_at);

CREATE TABLE IF NOT EXISTS departments (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    parent_id BIGINT NOT NULL DEFAULT 0,
    name VARCHAR(64) NOT NULL,
    sort INTEGER NOT NULL DEFAULT 0,
    leader VARCHAR(64),
    phone VARCHAR(32),
    status SMALLINT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_departments_deleted_at ON departments (deleted_at);

CREATE TABLE IF NOT EXISTS user_roles (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_user_role ON user_roles (user_id, role_id);

CREATE TABLE IF NOT EXISTS role_menus (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL,
    menu_id BIGINT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_role_menu ON role_menus (role_id, menu_id);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id BIGINT NOT NULL,
    token VARCHAR(256) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE,
    user_agent VARCHAR(255),
    ip VARCHAR(64)
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_token ON refresh_tokens (token);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens (expires_at);

CREATE TABLE IF NOT EXISTS operation_logs (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id BIGINT NOT NULL DEFAULT 0,
    username VARCHAR(64),
    module VARCHAR(64),
    action VARCHAR(64),
    method VARCHAR(16),
    path VARCHAR(255),
    query TEXT,
    body TEXT,
    status_code INTEGER,
    duration_ms BIGINT,
    ip VARCHAR(64),
    user_agent VARCHAR(255)
);
CREATE INDEX IF NOT EXISTS idx_operation_logs_created_at ON operation_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_operation_logs_username ON operation_logs (username);
CREATE INDEX IF NOT EXISTS idx_operation_logs_module ON operation_logs (module);
CREATE INDEX IF NOT EXISTS idx_operation_logs_status_code ON operation_logs (status_code);
