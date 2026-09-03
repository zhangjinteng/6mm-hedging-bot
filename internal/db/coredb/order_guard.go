package coredb

import (
	"context"
	"database/sql"
	"time"
)

// LatestOrderExecutionState 是自动对冲防重所需的最近订单执行状态。
type LatestOrderExecutionState struct {
	ConfigID        int64          `json:"config_id"`
	OrderPlanID     int64          `json:"order_plan_id"`
	ExecutionID     int64          `json:"execution_id"`
	ExchangeOrderID sql.NullString `json:"exchange_order_id"`
	ClientOrderID   string         `json:"client_order_id"`
	Symbol          string         `json:"symbol"`
	Side            string         `json:"side"`
	Status          string         `json:"status"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

const getLatestOrderExecutionStateByConfigID = `
SELECT
    plan.config_id,
    plan.id,
    execution.id,
    execution.exchange_order_id,
    execution.client_order_id,
    plan.symbol,
    plan.side,
    execution.status,
    execution.updated_at
FROM order_plans AS plan
JOIN order_executions AS execution ON execution.order_plan_id = plan.id
WHERE plan.config_id = $1
ORDER BY execution.updated_at DESC, execution.id DESC
LIMIT 1
`

const getOrderExecutionStateByExecutionID = `
SELECT
    plan.config_id,
    plan.id,
    execution.id,
    execution.exchange_order_id,
    execution.client_order_id,
    plan.symbol,
    plan.side,
    execution.status,
    execution.updated_at
FROM order_plans AS plan
JOIN order_executions AS execution ON execution.order_plan_id = plan.id
WHERE execution.id = $1
LIMIT 1
`

// GetLatestOrderExecutionStateByConfigID 查询单个配置最近一次订单执行状态。
func (q *Queries) GetLatestOrderExecutionStateByConfigID(ctx context.Context, configID int64) (LatestOrderExecutionState, error) {
	row := q.db.QueryRowContext(ctx, getLatestOrderExecutionStateByConfigID, configID)
	var state LatestOrderExecutionState
	err := row.Scan(
		&state.ConfigID,
		&state.OrderPlanID,
		&state.ExecutionID,
		&state.ExchangeOrderID,
		&state.ClientOrderID,
		&state.Symbol,
		&state.Side,
		&state.Status,
		&state.UpdatedAt,
	)
	return state, err
}

// GetOrderExecutionStateByExecutionID 查询指定订单执行及其订单计划上下文。
func (q *Queries) GetOrderExecutionStateByExecutionID(ctx context.Context, executionID int64) (LatestOrderExecutionState, error) {
	row := q.db.QueryRowContext(ctx, getOrderExecutionStateByExecutionID, executionID)
	var state LatestOrderExecutionState
	err := row.Scan(
		&state.ConfigID,
		&state.OrderPlanID,
		&state.ExecutionID,
		&state.ExchangeOrderID,
		&state.ClientOrderID,
		&state.Symbol,
		&state.Side,
		&state.Status,
		&state.UpdatedAt,
	)
	return state, err
}
