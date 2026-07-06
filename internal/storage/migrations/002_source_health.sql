ALTER TABLE sources ADD COLUMN last_error TEXT;
ALTER TABLE sources ADD COLUMN last_error_at TEXT;
ALTER TABLE sources ADD COLUMN error_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN paused_until TEXT;
