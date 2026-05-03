CREATE TABLE chain_gas_cache
(
    chain_id            TEXT PRIMARY KEY,
    base_gas            INTEGER NOT NULL,
    gas_per_output      INTEGER NOT NULL,
    fee_denom           TEXT    NOT NULL,
    gas_price_amount    TEXT    NOT NULL,
    sample_count        INTEGER NOT NULL,
    last_updated        INTEGER NOT NULL,
    last_failure_reason TEXT,
    last_failure_at     INTEGER,
    last_decay_at       INTEGER
);