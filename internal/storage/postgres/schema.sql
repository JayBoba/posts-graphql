CREATE TABLE IF NOT EXISTS posts (
    id               BIGSERIAL PRIMARY KEY,
    author_id        TEXT        NOT NULL,
    title            TEXT        NOT NULL,
    body             TEXT        NOT NULL,
    comments_enabled BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
    id         BIGSERIAL PRIMARY KEY,
    post_id    BIGINT      NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id  BIGINT      REFERENCES comments (id) ON DELETE CASCADE,
    author_id  TEXT        NOT NULL,
    body       TEXT        NOT NULL CHECK (char_length(body) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS comments_roots_idx ON comments (post_id, id) WHERE parent_id IS NULL;

CREATE INDEX IF NOT EXISTS comments_replies_idx ON comments (parent_id, id);