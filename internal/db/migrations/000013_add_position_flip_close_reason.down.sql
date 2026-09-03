UPDATE order_plans
SET reason = 'rebalance'
WHERE reason = 'position_flip_close';

ALTER TABLE order_plans
    DROP CONSTRAINT IF EXISTS order_plans_reason_check;

ALTER TABLE order_plans
    ADD CONSTRAINT order_plans_reason_check
    CHECK (reason IN (
        'first_trigger',
        'rebalance',
        'exit_hedge',
        'manual_close',
        'hedge_ratio_adjustment'
    )) NOT VALID;

ALTER TABLE order_plans
    VALIDATE CONSTRAINT order_plans_reason_check;

COMMENT ON COLUMN order_plans.reason IS
    '对冲执行类型：first_trigger 首次触发、rebalance 再平衡、exit_hedge 退出对冲、manual_close 手动平仓、hedge_ratio_adjustment 对冲比例调整。';
