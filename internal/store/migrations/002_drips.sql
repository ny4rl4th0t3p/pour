CREATE TABLE drips
(
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    chain_id          TEXT    NOT NULL,
    address           TEXT    NOT NULL,
    coins             TEXT    NOT NULL,
    tier              TEXT    NOT NULL,
    fallback_reason   TEXT,
    requester_ip      TEXT,
    api_key_id        TEXT,
    signature_address TEXT,
    tx_hash           TEXT,
    status            TEXT    NOT NULL,
    error             TEXT,
    requested_at      INTEGER NOT NULL,
    completed_at      INTEGER
);

CREATE INDEX idx_drips_address_chain ON drips (address, chain_id, requested_at);
CREATE INDEX idx_drips_chain_status ON drips (chain_id, status);
CREATE INDEX idx_drips_tier ON drips (tier, requested_at);