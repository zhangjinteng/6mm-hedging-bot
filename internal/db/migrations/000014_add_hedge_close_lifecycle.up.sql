ALTER TABLE hedge_configs
    ADD COLUMN IF NOT EXISTS lifecycle_status text NOT NULL DEFAULT 'active';

UPDATE hedge_configs
SET lifecycle_status = CASE WHEN enabled THEN 'active' ELSE 'disabled' END;

ALTER TABLE hedge_configs
    DROP CONSTRAINT IF EXISTS hedge_configs_lifecycle_status_check;

ALTER TABLE hedge_configs
    ADD CONSTRAINT hedge_configs_lifecycle_status_check
    CHECK (lifecycle_status IN ('active', 'closing', 'close_failed', 'disabled'));

CREATE TABLE IF NOT EXISTS hedge_close_requests (
    id bigserial PRIMARY KEY,
    config_id bigint NOT NULL REFERENCES hedge_configs(id),
    order_execution_id bigint REFERENCES order_executions(id),
    idempotency_key text NOT NULL,
    status text NOT NULL DEFAULT 'requested',
    error_message text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT hedge_close_requests_status_check CHECK (
        status IN ('requested', 'submitted', 'verifying', 'completed', 'failed')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS hedge_close_requests_idempotency_key_uidx
    ON hedge_close_requests (idempotency_key);

CREATE UNIQUE INDEX IF NOT EXISTS hedge_close_requests_active_config_uidx
    ON hedge_close_requests (config_id)
    WHERE status IN ('requested', 'submitted', 'verifying');

CREATE INDEX IF NOT EXISTS hedge_close_requests_config_created_at_idx
    ON hedge_close_requests (config_id, created_at DESC, id DESC);

DROP TRIGGER IF EXISTS hedge_close_requests_set_updated_at ON hedge_close_requests;
CREATE TRIGGER hedge_close_requests_set_updated_at
BEFORE UPDATE ON hedge_close_requests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON COLUMN hedge_configs.lifecycle_status IS
    '对冲配置生命周期：active 正常、closing 正在平仓、close_failed 平仓失败、disabled 已关闭。';
COMMENT ON TABLE hedge_close_requests IS '币种对冲异步关闭请求及其最终确认状态。';
