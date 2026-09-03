CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint PRIMARY KEY,
    name text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS exchange_accounts (
    id bigserial PRIMARY KEY,
    exchange text NOT NULL,
    name text NOT NULL,
    ccxt_id text NOT NULL DEFAULT '',
    market_type text NOT NULL DEFAULT 'swap',
    sandbox boolean NOT NULL DEFAULT true,
    default_settle text NOT NULL DEFAULT 'USDT',
    account_type text NOT NULL DEFAULT '',
    product_type text NOT NULL DEFAULT '',
    category text NOT NULL DEFAULT '',
    position_mode text NOT NULL DEFAULT 'one_way',
    margin_mode text NOT NULL DEFAULT 'cross',
    recv_window_ms integer NOT NULL DEFAULT 5000,
    rate_limit_ms integer NOT NULL DEFAULT 0,
    allowed_symbols jsonb NOT NULL DEFAULT '[]'::jsonb,
    api_key_encrypted text NOT NULL DEFAULT '',
    api_key_hint text NOT NULL DEFAULT '',
    api_secret_encrypted text NOT NULL DEFAULT '',
    passphrase_encrypted text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    is_primary boolean NOT NULL DEFAULT false,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT exchange_accounts_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT exchange_accounts_market_type_check CHECK (market_type IN ('spot', 'margin', 'swap', 'future')),
    CONSTRAINT exchange_accounts_position_mode_check CHECK (position_mode IN ('one_way', 'hedge')),
    CONSTRAINT exchange_accounts_margin_mode_check CHECK (margin_mode IN ('cross', 'isolated')),
    CONSTRAINT exchange_accounts_recv_window_check CHECK (recv_window_ms >= 0 AND recv_window_ms <= 60000),
    CONSTRAINT exchange_accounts_rate_limit_check CHECK (rate_limit_ms >= 0 AND rate_limit_ms <= 60000),
    CONSTRAINT exchange_accounts_allowed_symbols_array_check CHECK (jsonb_typeof(allowed_symbols) = 'array'),
    CONSTRAINT exchange_accounts_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_exchange_name_active_uidx
    ON exchange_accounts (exchange, name)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS exchange_accounts_primary_active_uidx
    ON exchange_accounts (is_primary)
    WHERE is_primary = true AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS exchange_accounts_exchange_status_active_idx
    ON exchange_accounts (exchange, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS exchange_accounts_deleted_at_idx
    ON exchange_accounts (deleted_at);

DROP TRIGGER IF EXISTS exchange_accounts_set_updated_at ON exchange_accounts;
CREATE TRIGGER exchange_accounts_set_updated_at
BEFORE UPDATE ON exchange_accounts
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS hedge_configs (
    id bigserial PRIMARY KEY,
    exchange_account_id bigint NOT NULL REFERENCES exchange_accounts(id),
    source text NOT NULL DEFAULT 'platform',
    symbol text NOT NULL,
    target_symbol text NOT NULL,
    target_hedge_ratio numeric(10, 6) NOT NULL DEFAULT 1,
    first_trigger_usdt numeric(38, 18) NOT NULL DEFAULT 5000,
    rebalance_usdt numeric(38, 18) NOT NULL DEFAULT 2000,
    exit_usdt numeric(38, 18) NOT NULL DEFAULT 1500,
    max_slippage_bps integer NOT NULL DEFAULT 30,
    max_notional_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    min_order_usdt numeric(38, 18) NOT NULL DEFAULT 10,
    leverage integer NOT NULL DEFAULT 1,
    enabled boolean NOT NULL DEFAULT true,
    dry_run boolean NOT NULL DEFAULT false,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT hedge_configs_ratio_check CHECK (target_hedge_ratio >= 0),
    CONSTRAINT hedge_configs_threshold_check CHECK (
        first_trigger_usdt >= 0
        AND rebalance_usdt >= 0
        AND exit_usdt >= 0
        AND max_notional_usdt >= 0
        AND min_order_usdt >= 0
    ),
    CONSTRAINT hedge_configs_slippage_check CHECK (max_slippage_bps >= 0),
    CONSTRAINT hedge_configs_leverage_check CHECK (leverage >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS hedge_configs_source_symbol_account_active_uidx
    ON hedge_configs (source, symbol, exchange_account_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS hedge_configs_deleted_at_idx
    ON hedge_configs (deleted_at);

DROP TRIGGER IF EXISTS hedge_configs_set_updated_at ON hedge_configs;
CREATE TRIGGER hedge_configs_set_updated_at
BEFORE UPDATE ON hedge_configs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS exposure_snapshots (
    id bigserial PRIMARY KEY,
    agent_id bigint NOT NULL DEFAULT 0,
    source text NOT NULL,
    symbol text NOT NULL,
    net_quantity numeric(38, 18) NOT NULL,
    net_notional_usdt numeric(38, 18) NOT NULL,
    mark_price numeric(38, 18) NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exposure_snapshots_mark_price_check CHECK (mark_price > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS exposure_snapshots_agent_source_symbol_uidx
    ON exposure_snapshots (agent_id, source, symbol);

CREATE INDEX IF NOT EXISTS exposure_snapshots_observed_at_idx
    ON exposure_snapshots (observed_at DESC);

DROP TRIGGER IF EXISTS exposure_snapshots_set_updated_at ON exposure_snapshots;
CREATE TRIGGER exposure_snapshots_set_updated_at
BEFORE UPDATE ON exposure_snapshots
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS hedge_position_snapshots (
    id bigserial PRIMARY KEY,
    agent_id bigint NOT NULL DEFAULT 0,
    exchange_account_id bigint NOT NULL DEFAULT 0,
    exchange text NOT NULL,
    account_name text NOT NULL,
    symbol text NOT NULL,
    position_side text NOT NULL DEFAULT 'NET',
    quantity numeric(38, 18) NOT NULL,
    notional_usdt numeric(38, 18) NOT NULL,
    entry_price numeric(38, 18) NOT NULL DEFAULT 0,
    mark_price numeric(38, 18) NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT hedge_position_snapshots_side_check CHECK (position_side IN ('NET', 'LONG', 'SHORT')),
    CONSTRAINT hedge_position_snapshots_mark_price_check CHECK (mark_price >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS hedge_position_snapshots_agent_account_symbol_uidx
    ON hedge_position_snapshots (agent_id, exchange_account_id, symbol);

CREATE INDEX IF NOT EXISTS hedge_position_snapshots_observed_at_idx
    ON hedge_position_snapshots (observed_at DESC);

DROP TRIGGER IF EXISTS hedge_position_snapshots_set_updated_at ON hedge_position_snapshots;
CREATE TRIGGER hedge_position_snapshots_set_updated_at
BEFORE UPDATE ON hedge_position_snapshots
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS hedge_monitor_snapshots (
    id bigserial PRIMARY KEY,
    agent_id bigint NOT NULL,
    config_id bigint NOT NULL REFERENCES hedge_configs(id),
    exchange_account_id bigint NOT NULL REFERENCES exchange_accounts(id),
    source text NOT NULL,
    symbol text NOT NULL,
    target_symbol text NOT NULL,
    exchange text NOT NULL,
    account_name text NOT NULL,
    net_quantity numeric(38, 18) NOT NULL DEFAULT 0,
    net_notional_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    target_hedge_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    actual_hedge_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    adjustment_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    switch_status text NOT NULL,
    health_status text NOT NULL,
    action_status text NOT NULL,
    status text NOT NULL,
    status_reason text NOT NULL DEFAULT '',
    exposure_observed_at timestamptz,
    position_observed_at timestamptz,
    calculated_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT hedge_monitor_snapshots_switch_status_check CHECK (
        switch_status IN ('enabled', 'global_off', 'symbol_off')
    ),
    CONSTRAINT hedge_monitor_snapshots_health_status_check CHECK (
        health_status IN ('ok', 'account_unavailable', 'observing', 'execution_failed')
    ),
    CONSTRAINT hedge_monitor_snapshots_action_status_check CHECK (
        action_status IN ('balanced', 'open_required', 'rebalance_required', 'exit_required')
    ),
    CONSTRAINT hedge_monitor_snapshots_status_check CHECK (
        status IN (
            'global_off',
            'symbol_off',
            'account_unavailable',
            'observing',
            'open_required',
            'rebalance_required',
            'exit_required',
            'balanced',
            'execution_failed'
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS hedge_monitor_snapshots_config_uidx
    ON hedge_monitor_snapshots (config_id);

CREATE INDEX IF NOT EXISTS hedge_monitor_snapshots_agent_status_idx
    ON hedge_monitor_snapshots (agent_id, status, symbol);

CREATE INDEX IF NOT EXISTS hedge_monitor_snapshots_agent_calculated_at_idx
    ON hedge_monitor_snapshots (agent_id, calculated_at DESC);

DROP TRIGGER IF EXISTS hedge_monitor_snapshots_set_updated_at ON hedge_monitor_snapshots;
CREATE TRIGGER hedge_monitor_snapshots_set_updated_at
BEFORE UPDATE ON hedge_monitor_snapshots
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS order_plans (
    id bigserial PRIMARY KEY,
    idempotency_key text NOT NULL,
    config_id bigint NOT NULL REFERENCES hedge_configs(id),
    agent_id bigint NOT NULL,
    exchange_account_id bigint NOT NULL REFERENCES exchange_accounts(id),
    source text NOT NULL,
    exchange text NOT NULL,
    account_name text NOT NULL,
    symbol text NOT NULL,
    side text NOT NULL,
    position_side text NOT NULL DEFAULT 'NET',
    order_type text NOT NULL DEFAULT 'LIMIT',
    quantity numeric(38, 18) NOT NULL,
    price numeric(38, 18) NOT NULL,
    notional_usdt numeric(38, 18) NOT NULL,
    reduce_only boolean NOT NULL DEFAULT false,
    reason text NOT NULL,
    status text NOT NULL DEFAULT 'planned',
    planned_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT order_plans_agent_id_check CHECK (agent_id > 0),
    CONSTRAINT order_plans_exchange_account_id_check CHECK (exchange_account_id > 0),
    CONSTRAINT order_plans_side_check CHECK (side IN ('BUY', 'SELL')),
    CONSTRAINT order_plans_position_side_check CHECK (position_side IN ('NET', 'LONG', 'SHORT')),
    CONSTRAINT order_plans_type_check CHECK (order_type IN ('LIMIT', 'MARKET')),
    CONSTRAINT order_plans_reason_check CHECK (reason IN ('first_trigger', 'rebalance', 'exit_hedge', 'manual_close', 'hedge_ratio_adjustment')),
    CONSTRAINT order_plans_status_check CHECK (status IN ('planned', 'skipped', 'submitted', 'filled', 'failed', 'dry_run')),
    CONSTRAINT order_plans_amount_check CHECK (quantity >= 0 AND price >= 0 AND notional_usdt >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS order_plans_idempotency_key_uidx
    ON order_plans (idempotency_key);

CREATE INDEX IF NOT EXISTS order_plans_config_created_at_idx
    ON order_plans (config_id, created_at DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_created_at_idx
    ON order_plans (agent_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_symbol_created_at_idx
    ON order_plans (agent_id, symbol, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_account_created_at_idx
    ON order_plans (agent_id, exchange_account_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_reason_created_at_idx
    ON order_plans (agent_id, reason, created_at DESC, id DESC);

DROP TRIGGER IF EXISTS order_plans_set_updated_at ON order_plans;
CREATE TRIGGER order_plans_set_updated_at
BEFORE UPDATE ON order_plans
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS order_executions (
    id bigserial PRIMARY KEY,
    order_plan_id bigint NOT NULL REFERENCES order_plans(id),
    exchange_order_id text,
    client_order_id text NOT NULL,
    status text NOT NULL,
    filled_quantity numeric(38, 18) NOT NULL DEFAULT 0,
    avg_price numeric(38, 18) NOT NULL DEFAULT 0,
    fee_usdt numeric(38, 18) NOT NULL DEFAULT 0,
    raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text,
    submitted_at timestamptz,
    filled_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT order_executions_status_check CHECK (status IN ('submitted', 'filled', 'failed', 'dry_run')),
    CONSTRAINT order_executions_amount_check CHECK (
        filled_quantity >= 0
        AND avg_price >= 0
        AND fee_usdt >= 0
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS order_executions_client_order_id_uidx
    ON order_executions (client_order_id);

CREATE UNIQUE INDEX IF NOT EXISTS order_executions_order_plan_id_uidx
    ON order_executions (order_plan_id);

DROP TRIGGER IF EXISTS order_executions_set_updated_at ON order_executions;
CREATE TRIGGER order_executions_set_updated_at
BEFORE UPDATE ON order_executions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS audit_events (
    id bigserial PRIMARY KEY,
    event_type text NOT NULL,
    severity text NOT NULL DEFAULT 'info',
    symbol text NOT NULL DEFAULT '',
    message text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_events_severity_check CHECK (severity IN ('info', 'warn', 'error'))
);

CREATE INDEX IF NOT EXISTS audit_events_created_at_idx
    ON audit_events (created_at DESC);

CREATE INDEX IF NOT EXISTS audit_events_type_symbol_idx
    ON audit_events (event_type, symbol);
