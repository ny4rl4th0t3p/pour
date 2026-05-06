ALTER TABLE drips ADD COLUMN mechanism TEXT NOT NULL DEFAULT 'anonymous';
CREATE INDEX idx_drips_mechanism ON drips (mechanism, chain_id, requested_at);