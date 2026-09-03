ALTER TABLE hedge_position_snapshots
    ADD COLUMN IF NOT EXISTS agent_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS exchange_account_id bigint NOT NULL DEFAULT 0;

UPDATE hedge_position_snapshots AS snapshot
SET agent_id = matched_account.agent_id,
    exchange_account_id = matched_account.id
FROM (
    SELECT DISTINCT ON (exchange, name)
        id,
        agent_id,
        exchange,
        name
    FROM exchange_accounts
    ORDER BY exchange, name, id
) AS matched_account
WHERE snapshot.exchange = matched_account.exchange
  AND snapshot.account_name = matched_account.name
  AND snapshot.exchange_account_id = 0;

ALTER TABLE hedge_position_snapshots
    DROP CONSTRAINT IF EXISTS hedge_position_snapshots_mark_price_check;

ALTER TABLE hedge_position_snapshots
    ADD CONSTRAINT hedge_position_snapshots_mark_price_check CHECK (mark_price >= 0);

DROP INDEX IF EXISTS hedge_position_snapshots_account_symbol_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS hedge_position_snapshots_agent_account_symbol_uidx
    ON hedge_position_snapshots (agent_id, exchange_account_id, symbol);

CREATE INDEX IF NOT EXISTS hedge_position_snapshots_agent_observed_at_idx
    ON hedge_position_snapshots (agent_id, observed_at DESC);

COMMENT ON COLUMN hedge_position_snapshots.agent_id IS '主代理商 ID，用于隔离不同代理商的真实对冲仓位。';
COMMENT ON COLUMN hedge_position_snapshots.exchange_account_id IS '交易所账户 ID；0 表示迁移前无法匹配账户的历史快照。';

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

COMMENT ON TABLE hedge_monitor_snapshots IS '后台敞口监控读模型，汇总上游敞口、真实对冲仓位和当前处置状态。';
COMMENT ON COLUMN hedge_monitor_snapshots.target_hedge_usdt IS '按目标比例和最大名义价值限制计算出的有符号目标对冲量。';
COMMENT ON COLUMN hedge_monitor_snapshots.actual_hedge_usdt IS '交易所当前有符号净对冲仓位名义价值。';
COMMENT ON COLUMN hedge_monitor_snapshots.adjustment_usdt IS '目标对冲量减去实际对冲量；页面可不展示，但决策计算仍会使用。';
COMMENT ON COLUMN hedge_monitor_snapshots.status IS '前端主状态；由开关、健康和动作状态按优先级合并得到。';
