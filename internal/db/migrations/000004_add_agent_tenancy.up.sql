ALTER TABLE exchange_accounts
    ADD COLUMN IF NOT EXISTS agent_id bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN exchange_accounts.agent_id IS '主代理商 ID；0 表示迁移前创建、尚未归属代理商的系统账户。';

DROP INDEX IF EXISTS exchange_accounts_exchange_name_active_uidx;
DROP INDEX IF EXISTS exchange_accounts_primary_active_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_agent_exchange_name_active_uidx
    ON exchange_accounts (agent_id, exchange, name)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_agent_primary_active_uidx
    ON exchange_accounts (agent_id)
    WHERE is_primary = true AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS exchange_accounts_agent_status_active_idx
    ON exchange_accounts (agent_id, status, id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_id_agent_uidx
    ON exchange_accounts (id, agent_id);

ALTER TABLE hedge_configs
    ADD COLUMN IF NOT EXISTS agent_id bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN hedge_configs.agent_id IS '主代理商 ID；必须与关联交易所账户的 agent_id 一致。';

UPDATE hedge_configs AS config
SET agent_id = account.agent_id
FROM exchange_accounts AS account
WHERE account.id = config.exchange_account_id
  AND config.agent_id IS DISTINCT FROM account.agent_id;

DROP INDEX IF EXISTS hedge_configs_source_symbol_account_active_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS hedge_configs_agent_source_symbol_account_active_uidx
    ON hedge_configs (agent_id, source, symbol, exchange_account_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS hedge_configs_agent_enabled_active_idx
    ON hedge_configs (agent_id, enabled, id)
    WHERE deleted_at IS NULL;

ALTER TABLE hedge_configs
    DROP CONSTRAINT IF EXISTS hedge_configs_exchange_account_agent_fk;

ALTER TABLE hedge_configs
    ADD CONSTRAINT hedge_configs_exchange_account_agent_fk
    FOREIGN KEY (exchange_account_id, agent_id)
    REFERENCES exchange_accounts (id, agent_id);

CREATE TABLE IF NOT EXISTS hedging_settings (
    agent_id bigint PRIMARY KEY,
    enabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT hedging_settings_agent_id_check CHECK (agent_id > 0)
);

COMMENT ON TABLE hedging_settings IS '代理商级对冲总开关配置。';
COMMENT ON COLUMN hedging_settings.agent_id IS '主代理商 ID。';
COMMENT ON COLUMN hedging_settings.enabled IS '是否允许调度该代理商已启用的对冲配置。';

DROP TRIGGER IF EXISTS hedging_settings_set_updated_at ON hedging_settings;
CREATE TRIGGER hedging_settings_set_updated_at
BEFORE UPDATE ON hedging_settings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
