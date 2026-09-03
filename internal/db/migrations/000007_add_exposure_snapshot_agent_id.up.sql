ALTER TABLE exposure_snapshots
    ADD COLUMN IF NOT EXISTS agent_id bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN exposure_snapshots.agent_id IS '主代理商 ID；用于隔离不同代理商在同一交易对上的净敞口。';

DROP INDEX IF EXISTS exposure_snapshots_source_symbol_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS exposure_snapshots_agent_source_symbol_uidx
    ON exposure_snapshots (agent_id, source, symbol);

CREATE INDEX IF NOT EXISTS exposure_snapshots_agent_observed_at_idx
    ON exposure_snapshots (agent_id, observed_at DESC);
