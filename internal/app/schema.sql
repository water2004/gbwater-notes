CREATE TABLE IF NOT EXISTS notes (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    course_slug TEXT NOT NULL,
    title TEXT NOT NULL,
    category TEXT NOT NULL,
    category_title TEXT NOT NULL,
    course TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 9999,
    draft BOOLEAN NOT NULL DEFAULT FALSE,
    content TEXT NOT NULL,
    plain_text TEXT NOT NULL,
    excerpt TEXT NOT NULL DEFAULT '',
    revision TEXT NOT NULL,
    modified_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS notes_course_idx ON notes(course_slug, position, id);

CREATE TABLE IF NOT EXISTS builds (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    state TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL,
    log TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS revisions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    title TEXT NOT NULL,
    action TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS revisions_note_idx ON revisions(note_id, id DESC);

CREATE TABLE IF NOT EXISTS note_views (
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    viewed_on DATE NOT NULL,
    views BIGINT NOT NULL CHECK (views >= 0),
    PRIMARY KEY (note_id, viewed_on)
);

CREATE INDEX IF NOT EXISTS note_views_date_idx ON note_views(viewed_on, views DESC);

CREATE TABLE IF NOT EXISTS site_settings (
    settings_key TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- 用户：邮箱与用户名都按 lower() 建唯一索引，避免大小写重复注册。
CREATE TABLE IF NOT EXISTS auth_policy (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    rate_enabled BOOLEAN NOT NULL,
    rate_attempts INTEGER NOT NULL CHECK (rate_attempts BETWEEN 1 AND 100),
    rate_window_minutes INTEGER NOT NULL CHECK (rate_window_minutes BETWEEN 1 AND 1440),
    email_mode TEXT NOT NULL CHECK (email_mode IN ('disabled', 'allowlist', 'denylist')),
    email_domains TEXT[] NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id                TEXT PRIMARY KEY,
    email             TEXT NOT NULL,
    email_verified_at TIMESTAMPTZ,
    username          TEXT NOT NULL,
    password_hash     TEXT NOT NULL,
    role              TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    last_login_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_idx ON users (lower(email));
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_idx ON users (lower(username));

-- 会话：只保存令牌的 SHA-256，原始令牌仅存在于浏览器 Cookie 中。
CREATE TABLE IF NOT EXISTS user_sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS user_sessions_user_idx ON user_sessions (user_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS user_sessions_expiry_idx ON user_sessions (expires_at);

-- 邮箱验证码：码值同样只保存 bcrypt 哈希，并记录失败次数与消费时间。
CREATE TABLE IF NOT EXISTS email_codes (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email       TEXT NOT NULL,
    purpose     TEXT NOT NULL CHECK (purpose IN ('register', 'reset')),
    code_hash   TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS email_codes_lookup_idx ON email_codes (email, purpose, created_at DESC);
CREATE INDEX IF NOT EXISTS email_codes_mailbox_idx ON email_codes (email, created_at DESC);

-- 阅读进度与收藏。note_id 刻意不加外键：内容以文件系统为准，
-- 笔记可能在同步前就被标记，删稿也不应连带删除用户数据。
CREATE TABLE IF NOT EXISTS user_note_progress (
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id      TEXT NOT NULL,
    completed    BOOLEAN NOT NULL DEFAULT FALSE,
    last_read_at TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, note_id)
);

CREATE INDEX IF NOT EXISTS user_note_progress_recent_idx ON user_note_progress (user_id, last_read_at DESC);

CREATE TABLE IF NOT EXISTS user_favorites (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, note_id)
);

CREATE INDEX IF NOT EXISTS user_favorites_recent_idx ON user_favorites (user_id, created_at DESC);
-- 用户标注：划线（含批注）、位置书签、单篇私人便条。
-- 锚点用「原文引用 + 前后文 + 标题」保存。读者打开不同版本的正文时，
-- 客户端据此计算显示位置，保留数据库里的原始锚点。
CREATE TABLE IF NOT EXISTS user_highlights (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id    TEXT NOT NULL,
    quote      TEXT NOT NULL,
    prefix     TEXT NOT NULL DEFAULT '',
    suffix     TEXT NOT NULL DEFAULT '',
    heading    TEXT NOT NULL DEFAULT '',
    color      TEXT NOT NULL DEFAULT 'yellow' CHECK (color IN ('yellow', 'green', 'blue', 'pink')),
    comment    TEXT NOT NULL DEFAULT '',
    revision   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS user_highlights_user_idx ON user_highlights (user_id, note_id, created_at);

CREATE TABLE IF NOT EXISTS user_bookmarks (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id    TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    quote      TEXT NOT NULL DEFAULT '',
    prefix     TEXT NOT NULL DEFAULT '',
    suffix     TEXT NOT NULL DEFAULT '',
    heading    TEXT NOT NULL DEFAULT '',
    position   DOUBLE PRECISION NOT NULL DEFAULT 0,
    revision   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS user_bookmarks_user_idx ON user_bookmarks (user_id, note_id, created_at DESC);

CREATE TABLE IF NOT EXISTS user_memos (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id    TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, note_id)
);
-- 便条：一篇笔记下可以有多张卡片，与「笔记」（单篇长文）区分开。
CREATE TABLE IF NOT EXISTS user_sticky_notes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id    TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    color      TEXT NOT NULL DEFAULT 'yellow' CHECK (color IN ('yellow', 'green', 'blue', 'pink')),
    quote      TEXT NOT NULL DEFAULT '',
    prefix     TEXT NOT NULL DEFAULT '',
    suffix     TEXT NOT NULL DEFAULT '',
    heading    TEXT NOT NULL DEFAULT '',
    revision   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS user_sticky_notes_user_idx ON user_sticky_notes (user_id, note_id, created_at DESC);
-- 便条锚定在正文位置上：与划线一样用引用 + 上下文 + revision 定位。
-- 用户个性化设置（阅读字号、页宽、行距、字体等），JSON 存一份避免频繁加列。
CREATE TABLE IF NOT EXISTS user_settings (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    reading    JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL
);
