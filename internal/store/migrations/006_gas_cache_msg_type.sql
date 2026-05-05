-- Add per-chain columns for MsgMultiSend gas learning.
-- Keeps the existing chain_id PRIMARY KEY and existing single-send columns untouched.
ALTER TABLE chain_gas_cache
    ADD COLUMN multisend_gas_per_output INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chain_gas_cache
    ADD COLUMN multisend_sample_count INTEGER NOT NULL DEFAULT 0;