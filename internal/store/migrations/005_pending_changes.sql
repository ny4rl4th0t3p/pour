CREATE TABLE pending_changes
(
    chain_id    TEXT    NOT NULL,
    field       TEXT    NOT NULL,
    old_value   TEXT,
    new_value   TEXT,
    source      TEXT    NOT NULL,
    detected_at INTEGER NOT NULL,
    severity    TEXT    NOT NULL,
    PRIMARY KEY (chain_id, field)
);