UPDATE order_plans
SET reason = CASE reason
    WHEN 'net exposure reached first trigger threshold' THEN 'first_trigger'
    WHEN 'hedge position drift reached rebalance check' THEN 'rebalance'
    WHEN 'net exposure is below exit threshold' THEN 'exit_hedge'
    ELSE reason
END
WHERE reason IN (
    'net exposure reached first trigger threshold',
    'hedge position drift reached rebalance check',
    'net exposure is below exit threshold'
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM order_plans
        WHERE reason NOT IN (
            'first_trigger',
            'rebalance',
            'exit_hedge',
            'manual_close',
            'hedge_ratio_adjustment'
        )
    ) THEN
        RAISE EXCEPTION 'cannot constrain order_plans.reason: unsupported values exist';
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'order_plans'::regclass
          AND conname = 'order_plans_reason_check'
    ) THEN
        ALTER TABLE order_plans
            ADD CONSTRAINT order_plans_reason_check
            CHECK (reason IN (
                'first_trigger',
                'rebalance',
                'exit_hedge',
                'manual_close',
                'hedge_ratio_adjustment'
            )) NOT VALID;
    END IF;
END
$$;

ALTER TABLE order_plans VALIDATE CONSTRAINT order_plans_reason_check;

CREATE INDEX IF NOT EXISTS order_plans_agent_reason_created_at_idx
    ON order_plans (agent_id, reason, created_at DESC, id DESC);

COMMENT ON COLUMN order_plans.reason IS
    '对冲执行类型：first_trigger 首次触发、rebalance 再平衡、exit_hedge 退出对冲、manual_close 手动平仓、hedge_ratio_adjustment 对冲比例调整。';
