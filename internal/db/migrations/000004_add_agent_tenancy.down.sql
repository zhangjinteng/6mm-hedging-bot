DROP TRIGGER IF EXISTS hedging_settings_set_updated_at ON hedging_settings;
DROP TABLE IF EXISTS hedging_settings;

ALTER TABLE hedge_configs
    DROP CONSTRAINT IF EXISTS hedge_configs_exchange_account_agent_fk;

DROP INDEX IF EXISTS hedge_configs_agent_enabled_active_idx;
DROP INDEX IF EXISTS hedge_configs_agent_source_symbol_account_active_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS hedge_configs_source_symbol_account_active_uidx
    ON hedge_configs (source, symbol, exchange_account_id)
    WHERE deleted_at IS NULL;

ALTER TABLE hedge_configs DROP COLUMN IF EXISTS agent_id;

DROP INDEX IF EXISTS exchange_accounts_id_agent_uidx;
DROP INDEX IF EXISTS exchange_accounts_agent_status_active_idx;
DROP INDEX IF EXISTS exchange_accounts_agent_primary_active_uidx;
DROP INDEX IF EXISTS exchange_accounts_agent_exchange_name_active_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_exchange_name_active_uidx
    ON exchange_accounts (exchange, name)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_primary_active_uidx
    ON exchange_accounts (is_primary)
    WHERE is_primary = true AND deleted_at IS NULL;

ALTER TABLE exchange_accounts DROP COLUMN IF EXISTS agent_id;
