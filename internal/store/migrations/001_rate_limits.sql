CREATE TABLE rate_limits
(
    scope_type    TEXT    NOT NULL,
    scope_value   TEXT    NOT NULL,
    chain_id      TEXT    NOT NULL,
    window_start  INTEGER NOT NULL,
    request_count INTEGER NOT NULL,
    coins_total   TEXT    NOT NULL,
    PRIMARY KEY (scope_type, scope_value, chain_id, window_start)
);

CREATE INDEX idx_rate_limits_lookup ON rate_limits (scope_type, scope_value, chain_id);