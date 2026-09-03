package grpcapi

import (
	"database/sql"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/hedge"
	hedgingv1 "github.com/zhangjinteng/6mm-hedging-bot/internal/pb/hedging/v1"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

func runHedgeResponse(result service.RunResult) *hedgingv1.RunHedgeResponse {
	resp := &hedgingv1.RunHedgeResponse{
		ConfigId: uint64(result.Config.ID),
		Exposure: exposureSnapshot(result.Exposure),
		Decision: hedgeDecision(result.Decision),
	}
	if result.Position != nil {
		resp.Position = positionSnapshot(result.Position)
	}
	if result.OrderPlan != nil {
		resp.OrderPlan = orderPlan(result.OrderPlan)
	}
	if result.Execution != nil {
		resp.Execution = orderExecution(result.Execution)
	}
	return resp
}

func exposureSnapshot(exposure coredb.ExposureSnapshot) *hedgingv1.ExposureSnapshot {
	return &hedgingv1.ExposureSnapshot{
		Id:              uint64(exposure.ID),
		AgentId:         uint64(exposure.AgentID),
		Source:          exposure.Source,
		Symbol:          exposure.Symbol,
		NetQuantity:     exposure.NetQuantity.String(),
		NetNotionalUsdt: exposure.NetNotionalUsdt.String(),
		MarkPrice:       exposure.MarkPrice.String(),
		ObservedAt:      ts(exposure.ObservedAt),
		CreatedAt:       ts(exposure.CreatedAt),
		UpdatedAt:       ts(exposure.UpdatedAt),
	}
}

func positionSnapshot(position *coredb.HedgePositionSnapshot) *hedgingv1.PositionSnapshot {
	if position == nil {
		return nil
	}
	return &hedgingv1.PositionSnapshot{
		Id:                uint64(position.ID),
		AgentId:           uint64(position.AgentID),
		ExchangeAccountId: uint64(position.ExchangeAccountID),
		Exchange:          position.Exchange,
		AccountName:       position.AccountName,
		Symbol:            position.Symbol,
		PositionSide:      position.PositionSide,
		Quantity:          position.Quantity.String(),
		NotionalUsdt:      position.NotionalUsdt.String(),
		EntryPrice:        position.EntryPrice.String(),
		MarkPrice:         position.MarkPrice.String(),
		ObservedAt:        ts(position.ObservedAt),
		CreatedAt:         ts(position.CreatedAt),
		UpdatedAt:         ts(position.UpdatedAt),
	}
}

func hedgeDecision(decision hedge.Decision) *hedgingv1.HedgeDecision {
	return &hedgingv1.HedgeDecision{
		Action:              string(decision.Action),
		Side:                string(decision.Side),
		Symbol:              decision.Symbol,
		TargetNotionalUsdt:  decision.TargetNotionalUSDT.String(),
		CurrentNotionalUsdt: decision.CurrentNotionalUSDT.String(),
		AdjustmentUsdt:      decision.AdjustmentUSDT.String(),
		OrderQuantity:       decision.OrderQuantity.String(),
		LimitPrice:          decision.LimitPrice.String(),
		ReduceOnly:          decision.ReduceOnly,
		Reason:              decision.Reason,
		DryRun:              decision.DryRun,
	}
}

func orderPlan(plan *coredb.OrderPlan) *hedgingv1.OrderPlan {
	if plan == nil {
		return nil
	}
	return &hedgingv1.OrderPlan{
		Id:             uint64(plan.ID),
		IdempotencyKey: plan.IdempotencyKey,
		ConfigId:       uint64(plan.ConfigID),
		Source:         plan.Source,
		Exchange:       plan.Exchange,
		AccountName:    plan.AccountName,
		Symbol:         plan.Symbol,
		Side:           plan.Side,
		PositionSide:   plan.PositionSide,
		OrderType:      plan.OrderType,
		Quantity:       plan.Quantity.String(),
		Price:          plan.Price.String(),
		NotionalUsdt:   plan.NotionalUsdt.String(),
		ReduceOnly:     plan.ReduceOnly,
		Reason:         plan.Reason,
		Status:         plan.Status,
		CreatedAt:      ts(plan.CreatedAt),
		UpdatedAt:      ts(plan.UpdatedAt),
	}
}

func orderExecution(execution *coredb.OrderExecution) *hedgingv1.OrderExecution {
	if execution == nil {
		return nil
	}
	return &hedgingv1.OrderExecution{
		Id:              uint64(execution.ID),
		OrderPlanId:     uint64(execution.OrderPlanID),
		ExchangeOrderId: nullString(execution.ExchangeOrderID),
		ClientOrderId:   execution.ClientOrderID,
		Status:          execution.Status,
		FilledQuantity:  execution.FilledQuantity.String(),
		AvgPrice:        execution.AvgPrice.String(),
		FeeUsdt:         execution.FeeUsdt.String(),
		ErrorMessage:    nullString(execution.ErrorMessage),
		CreatedAt:       ts(execution.CreatedAt),
		UpdatedAt:       ts(execution.UpdatedAt),
	}
}

func ts(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
