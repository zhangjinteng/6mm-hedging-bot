ALTER TABLE order_plans
    ADD COLUMN IF NOT EXISTS agent_id bigint,
    ADD COLUMN IF NOT EXISTS exchange_account_id bigint,
    ADD COLUMN IF NOT EXISTS planned_at timestamptz;

UPDATE order_plans AS plan
SET agent_id = config.agent_id,
    exchange_account_id = config.exchange_account_id,
    planned_at = COALESCE(plan.planned_at, plan.created_at)
FROM hedge_configs AS config
WHERE config.id = plan.config_id
  AND (
      plan.agent_id IS NULL
      OR plan.exchange_account_id IS NULL
      OR plan.planned_at IS NULL
  );

UPDATE order_plans
SET planned_at = created_at
WHERE planned_at IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM order_plans
        WHERE agent_id IS NULL
           OR exchange_account_id IS NULL
    ) THEN
        RAISE EXCEPTION
            'cannot backfill order_plans ownership: one or more config_id values do not reference hedge_configs';
    END IF;
END
$$;

ALTER TABLE order_plans
    ALTER COLUMN agent_id SET NOT NULL,
    ALTER COLUMN exchange_account_id SET NOT NULL,
    ALTER COLUMN planned_at SET DEFAULT now(),
    ALTER COLUMN planned_at SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'order_plans'::regclass
          AND conname = 'order_plans_config_id_fkey'
    ) THEN
        ALTER TABLE order_plans
            ADD CONSTRAINT order_plans_config_id_fkey
            FOREIGN KEY (config_id)
            REFERENCES hedge_configs(id)
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'order_plans'::regclass
          AND conname = 'order_plans_exchange_account_id_fkey'
    ) THEN
        ALTER TABLE order_plans
            ADD CONSTRAINT order_plans_exchange_account_id_fkey
            FOREIGN KEY (exchange_account_id)
            REFERENCES exchange_accounts(id)
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'order_plans'::regclass
          AND conname = 'order_plans_agent_id_check'
    ) THEN
        ALTER TABLE order_plans
            ADD CONSTRAINT order_plans_agent_id_check
            CHECK (agent_id > 0)
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'order_plans'::regclass
          AND conname = 'order_plans_exchange_account_id_check'
    ) THEN
        ALTER TABLE order_plans
            ADD CONSTRAINT order_plans_exchange_account_id_check
            CHECK (exchange_account_id > 0)
            NOT VALID;
    END IF;
END
$$;

ALTER TABLE order_plans VALIDATE CONSTRAINT order_plans_config_id_fkey;
ALTER TABLE order_plans VALIDATE CONSTRAINT order_plans_exchange_account_id_fkey;
ALTER TABLE order_plans VALIDATE CONSTRAINT order_plans_agent_id_check;
ALTER TABLE order_plans VALIDATE CONSTRAINT order_plans_exchange_account_id_check;

CREATE INDEX IF NOT EXISTS order_plans_agent_created_at_idx
    ON order_plans (agent_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_symbol_created_at_idx
    ON order_plans (agent_id, symbol, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS order_plans_agent_account_created_at_idx
    ON order_plans (agent_id, exchange_account_id, created_at DESC, id DESC);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM order_executions
        GROUP BY order_plan_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot enforce one execution per order plan: duplicate order_plan_id values exist';
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS order_executions_order_plan_id_uidx
    ON order_executions (order_plan_id);

ALTER TABLE order_executions
    ADD COLUMN IF NOT EXISTS submitted_at timestamptz,
    ADD COLUMN IF NOT EXISTS filled_at timestamptz,
    ADD COLUMN IF NOT EXISTS failed_at timestamptz;

UPDATE order_executions
SET submitted_at = CASE
        WHEN status IN ('submitted', 'filled') THEN COALESCE(submitted_at, created_at)
        ELSE submitted_at
    END,
    filled_at = CASE
        WHEN status = 'filled' THEN COALESCE(filled_at, updated_at, created_at)
        ELSE filled_at
    END,
    failed_at = CASE
        WHEN status = 'failed' THEN COALESCE(failed_at, updated_at, created_at)
        ELSE failed_at
    END;

COMMENT ON COLUMN order_plans.agent_id IS '生成订单计划时的主代理商 ID 快照。';
COMMENT ON COLUMN order_plans.exchange_account_id IS '生成订单计划时使用的交易所账户 ID 快照。';
COMMENT ON COLUMN order_plans.planned_at IS '订单计划生成时间。';
COMMENT ON COLUMN order_executions.submitted_at IS '订单成功提交至交易所的时间。';
COMMENT ON COLUMN order_executions.filled_at IS '订单成交完成时间。';
COMMENT ON COLUMN order_executions.failed_at IS '订单执行失败时间。';
