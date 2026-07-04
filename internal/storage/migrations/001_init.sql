CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    entity_ref TEXT NOT NULL,
    public_username TEXT,
    title TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS source_states (
    source_id TEXT PRIMARY KEY,
    last_message_id INTEGER NOT NULL DEFAULT 0,
    last_comment_message_id INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE TABLE IF NOT EXISTS messages (
    external_id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    source_label TEXT NOT NULL,
    chat_id INTEGER NOT NULL,
    chat_title TEXT,
    message_id INTEGER NOT NULL,
    sender_id INTEGER,
    sender_username TEXT,
    sender_username_normalized TEXT,
    sender_display_name TEXT,
    text TEXT,
    link TEXT,
    date TEXT,
    hidden_by_exclusion INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE INDEX IF NOT EXISTS idx_messages_source_date ON messages(source_id, date);
CREATE INDEX IF NOT EXISTS idx_messages_sender_id ON messages(sender_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender_username ON messages(sender_username_normalized);
CREATE INDEX IF NOT EXISTS idx_messages_hidden_date ON messages(hidden_by_exclusion, date);

CREATE TABLE IF NOT EXISTS excluded_senders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    sender_id INTEGER,
    username TEXT,
    username_normalized TEXT,
    display_name TEXT,

    reason TEXT,

    evidence_message_external_id TEXT,
    evidence_message_text TEXT,
    evidence_message_link TEXT,
    evidence_message_date TEXT,
    evidence_source_id TEXT,
    evidence_source_title TEXT,

    scope_type TEXT NOT NULL DEFAULT 'global',
    source_id TEXT,

    created_at TEXT NOT NULL,
    created_by TEXT,

    UNIQUE(sender_id, scope_type, source_id),
    UNIQUE(username_normalized, scope_type, source_id)
);

CREATE INDEX IF NOT EXISTS idx_excluded_senders_sender_id ON excluded_senders(sender_id);
CREATE INDEX IF NOT EXISTS idx_excluded_senders_username ON excluded_senders(username_normalized);
CREATE INDEX IF NOT EXISTS idx_excluded_senders_scope ON excluded_senders(scope_type, source_id);
