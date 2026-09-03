DROP INDEX IF EXISTS order_executions_order_plan_id_uidx;

ALTER TABLE order_executions
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS filled_at,
    DROP COLUMN IF EXISTS submitted_at;

DROP INDEX IF EXISTS order_plans_agent_account_created_at_idx;
DROP INDEX IF EXISTS order_plans_agent_symbol_created_at_idx;
DROP INDEX IF EXISTS order_plans_agent_created_at_idx;

ALTER TABLE order_plans
    DROP CONSTRAINT IF EXISTS order_plans_exchange_account_id_check,
    DROP CONSTRAINT IF EXISTS order_plans_agent_id_check,
    DROP CONSTRAINT IF EXISTS order_plans_exchange_account_id_fkey,
    DROP CONSTRAINT IF EXISTS order_plans_config_id_fkey,
    DROP COLUMN IF EXISTS planned_at,
    DROP COLUMN IF EXISTS exchange_account_id,
    DROP COLUMN IF EXISTS agent_id;
