DROP TRIGGER IF EXISTS hedge_monitor_snapshots_set_updated_at ON hedge_monitor_snapshots;
DROP TABLE IF EXISTS hedge_monitor_snapshots;

DROP INDEX IF EXISTS hedge_position_snapshots_agent_observed_at_idx;
DROP INDEX IF EXISTS hedge_position_snapshots_agent_account_symbol_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS hedge_position_snapshots_account_symbol_uidx
    ON hedge_position_snapshots (exchange, account_name, symbol);

ALTER TABLE hedge_position_snapshots
    DROP CONSTRAINT IF EXISTS hedge_position_snapshots_mark_price_check;

ALTER TABLE hedge_position_snapshots
    ADD CONSTRAINT hedge_position_snapshots_mark_price_check CHECK (mark_price > 0);

ALTER TABLE hedge_position_snapshots
    DROP COLUMN IF EXISTS exchange_account_id,
    DROP COLUMN IF EXISTS agent_id;
