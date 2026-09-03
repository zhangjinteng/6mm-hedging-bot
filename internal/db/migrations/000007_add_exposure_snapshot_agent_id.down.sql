DROP INDEX IF EXISTS exposure_snapshots_agent_observed_at_idx;
DROP INDEX IF EXISTS exposure_snapshots_agent_source_symbol_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS exposure_snapshots_source_symbol_uidx
    ON exposure_snapshots (source, symbol);

ALTER TABLE exposure_snapshots
    DROP COLUMN IF EXISTS agent_id;
