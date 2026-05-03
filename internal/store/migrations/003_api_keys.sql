CREATE TABLE api_keys
(
    id                  TEXT PRIMARY KEY,
    secret_hash         BLOB    NOT NULL,
    label               TEXT,
    chain_scope         TEXT    NOT NULL,
    per_chain_drips     TEXT,
    rate_limit_per_hour INTEGER,
    expires_at          INTEGER,
    created_at          INTEGER NOT NULL,
    last_used_at        INTEGER,
    revoked_at          INTEGER
);