DROP INDEX IF EXISTS order_plans_agent_reason_created_at_idx;

ALTER TABLE order_plans
    DROP CONSTRAINT IF EXISTS order_plans_reason_check;

COMMENT ON COLUMN order_plans.reason IS '生成该订单计划的原因说明。';
