-- +goose Up
CREATE TABLE agent_state (
    namespace TEXT NOT NULL,
    key TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1,
    payload TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (namespace, key)
);
-- +goose Down
DROP TABLE agent_state;

