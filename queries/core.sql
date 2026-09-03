-- name: UpsertExposureSnapshot :one
INSERT INTO exposure_snapshots (
    agent_id,
    source,
    symbol,
    net_quantity,
    net_notional_usdt,
    mark_price,
    observed_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
)
ON CONFLICT (agent_id, source, symbol) DO UPDATE SET
    net_quantity = EXCLUDED.net_quantity,
    net_notional_usdt = EXCLUDED.net_notional_usdt,
    mark_price = EXCLUDED.mark_price,
    observed_at = EXCLUDED.observed_at
RETURNING *;

-- name: GetExposureSnapshot :one
SELECT *
FROM exposure_snapshots
WHERE agent_id = $1
  AND source = $2
  AND symbol = $3;

-- name: ListExposureSnapshots :many
SELECT *
FROM exposure_snapshots
WHERE source = $1
ORDER BY observed_at DESC, symbol ASC;

-- name: ListAllExposureSnapshots :many
SELECT *
FROM exposure_snapshots
ORDER BY agent_id ASC, source ASC, symbol ASC;

-- name: UpsertHedgePositionSnapshot :one
INSERT INTO hedge_position_snapshots (
    agent_id,
    exchange_account_id,
    exchange,
    account_name,
    symbol,
    position_side,
    quantity,
    notional_usdt,
    entry_price,
    mark_price,
    observed_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11
)
ON CONFLICT (agent_id, exchange_account_id, symbol) DO UPDATE SET
    exchange = EXCLUDED.exchange,
    account_name = EXCLUDED.account_name,
    position_side = EXCLUDED.position_side,
    quantity = EXCLUDED.quantity,
    notional_usdt = EXCLUDED.notional_usdt,
    entry_price = EXCLUDED.entry_price,
    mark_price = EXCLUDED.mark_price,
    observed_at = EXCLUDED.observed_at
RETURNING *;

-- name: GetHedgePositionSnapshot :one
SELECT *
FROM hedge_position_snapshots
WHERE agent_id = $1
  AND exchange_account_id = $2
  AND symbol = $3;

-- name: ListAllHedgePositionSnapshots :many
SELECT *
FROM hedge_position_snapshots
ORDER BY agent_id ASC, exchange_account_id ASC, symbol ASC;

-- name: ListLatestOrderExecutionStates :many
SELECT DISTINCT ON (plan.config_id)
    plan.config_id,
    execution.status,
    COALESCE(execution.error_message, '')::text AS error_message,
    execution.updated_at
FROM order_plans AS plan
JOIN order_executions AS execution ON execution.order_plan_id = plan.id
ORDER BY plan.config_id, execution.updated_at DESC, execution.id DESC;

-- name: UpsertHedgeMonitorSnapshot :one
INSERT INTO hedge_monitor_snapshots (
    agent_id,
    config_id,
    exchange_account_id,
    source,
    symbol,
    target_symbol,
    exchange,
    account_name,
    net_quantity,
    net_notional_usdt,
    target_hedge_usdt,
    actual_hedge_usdt,
    adjustment_usdt,
    switch_status,
    health_status,
    action_status,
    status,
    status_reason,
    exposure_observed_at,
    position_observed_at,
    calculated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20, $21
)
ON CONFLICT (config_id) DO UPDATE SET
    agent_id = EXCLUDED.agent_id,
    exchange_account_id = EXCLUDED.exchange_account_id,
    source = EXCLUDED.source,
    symbol = EXCLUDED.symbol,
    target_symbol = EXCLUDED.target_symbol,
    exchange = EXCLUDED.exchange,
    account_name = EXCLUDED.account_name,
    net_quantity = EXCLUDED.net_quantity,
    net_notional_usdt = EXCLUDED.net_notional_usdt,
    target_hedge_usdt = EXCLUDED.target_hedge_usdt,
    actual_hedge_usdt = EXCLUDED.actual_hedge_usdt,
    adjustment_usdt = EXCLUDED.adjustment_usdt,
    switch_status = EXCLUDED.switch_status,
    health_status = EXCLUDED.health_status,
    action_status = EXCLUDED.action_status,
    status = EXCLUDED.status,
    status_reason = EXCLUDED.status_reason,
    exposure_observed_at = EXCLUDED.exposure_observed_at,
    position_observed_at = EXCLUDED.position_observed_at,
    calculated_at = EXCLUDED.calculated_at
RETURNING *;

-- name: ListHedgeMonitorSnapshots :many
SELECT *
FROM hedge_monitor_snapshots
WHERE agent_id = $1
ORDER BY symbol ASC, config_id ASC;

-- name: PruneHedgeMonitorSnapshots :execrows
DELETE FROM hedge_monitor_snapshots
WHERE NOT EXISTS (
    SELECT 1
    FROM hedge_configs
    WHERE hedge_configs.id = hedge_monitor_snapshots.config_id
      AND hedge_configs.deleted_at IS NULL
);

-- name: CreateOrderPlan :one
INSERT INTO order_plans (
    idempotency_key,
    config_id,
    agent_id,
    exchange_account_id,
    source,
    exchange,
    account_name,
    symbol,
    side,
    position_side,
    order_type,
    quantity,
    price,
    notional_usdt,
    reduce_only,
    reason,
    status
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12,
    $13,
    $14,
    $15,
    $16,
    $17
)
ON CONFLICT (idempotency_key) DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: UpdateOrderPlanStatus :one
UPDATE order_plans
SET status = $2
WHERE id = $1
RETURNING *;

-- name: CreateOrderExecution :one
INSERT INTO order_executions (
    order_plan_id,
    exchange_order_id,
    client_order_id,
    status,
    filled_quantity,
    avg_price,
    fee_usdt,
    raw_response,
    error_message,
    submitted_at,
    filled_at,
    failed_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    CASE WHEN $4 IN ('submitted', 'filled') THEN now() ELSE NULL END,
    CASE WHEN $4 = 'filled' THEN now() ELSE NULL END,
    CASE WHEN $4 = 'failed' THEN now() ELSE NULL END
)
ON CONFLICT (order_plan_id) DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: GetOrderExecutionByClientOrderID :one
SELECT *
FROM order_executions
WHERE client_order_id = $1;

-- name: UpdateOrderExecutionStatus :one
UPDATE order_executions
SET
    exchange_order_id = $2,
    status = $3,
    filled_quantity = $4,
    avg_price = $5,
    fee_usdt = $6,
    raw_response = $7,
    error_message = $8,
    submitted_at = CASE
        WHEN $3 IN ('submitted', 'filled') THEN COALESCE(submitted_at, now())
        ELSE submitted_at
    END,
    filled_at = CASE
        WHEN $3 = 'filled' THEN COALESCE(filled_at, now())
        ELSE filled_at
    END,
    failed_at = CASE
        WHEN $3 = 'failed' THEN COALESCE(failed_at, now())
        ELSE failed_at
    END
WHERE id = $1
RETURNING *;

-- name: CreateAuditEvent :one
INSERT INTO audit_events (
    event_type,
    severity,
    symbol,
    message,
    payload
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
RETURNING *;

-- name: ListRecentAuditEvents :many
SELECT *
FROM audit_events
ORDER BY created_at DESC
LIMIT $1;
