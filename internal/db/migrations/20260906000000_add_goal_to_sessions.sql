-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN goal TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN goal;
-- +goose StatementEnd
