package service

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/hedge"
)

type hedgeStoreStub struct {
	coredb.Querier
	state            coredb.LatestOrderExecutionState
	stateErr         error
	position         coredb.HedgePositionSnapshot
	positionErr      error
	upsertParams     []coredb.UpsertHedgePositionSnapshotParams
	auditEvents      int
	executionUpdates []coredb.UpdateOrderExecutionStatusParams
	planUpdates      []coredb.UpdateOrderPlanStatusParams
	updatedAt        time.Time
}

func (store *hedgeStoreStub) CreateAuditEvent(context.Context, coredb.CreateAuditEventParams) (coredb.AuditEvent, error) {
	store.auditEvents++
	return coredb.AuditEvent{}, nil
}

func (store *hedgeStoreStub) UpdateOrderExecutionStatus(_ context.Context, params coredb.UpdateOrderExecutionStatusParams) (coredb.OrderExecution, error) {
	store.executionUpdates = append(store.executionUpdates, params)
	return coredb.OrderExecution{
		ID:             params.ID,
		Status:         params.Status,
		FilledQuantity: params.FilledQuantity,
		AvgPrice:       params.AvgPrice,
		FeeUsdt:        params.FeeUsdt,
		UpdatedAt:      store.updatedAt,
	}, nil
}

func (store *hedgeStoreStub) UpdateOrderPlanStatus(_ context.Context, params coredb.UpdateOrderPlanStatusParams) (coredb.OrderPlan, error) {
	store.planUpdates = append(store.planUpdates, params)
	return coredb.OrderPlan{ID: params.ID, Status: params.Status}, nil
}

func (store *hedgeStoreStub) GetLatestOrderExecutionStateByConfigID(context.Context, int64) (coredb.LatestOrderExecutionState, error) {
	return store.state, store.stateErr
}

func (store *hedgeStoreStub) GetHedgePositionSnapshot(context.Context, coredb.GetHedgePositionSnapshotParams) (coredb.HedgePositionSnapshot, error) {
	return store.position, store.positionErr
}

func (store *hedgeStoreStub) UpsertHedgePositionSnapshot(_ context.Context, params coredb.UpsertHedgePositionSnapshotParams) (coredb.HedgePositionSnapshot, error) {
	store.upsertParams = append(store.upsertParams, params)
	return coredb.HedgePositionSnapshot{
		AgentID:           params.AgentID,
		ExchangeAccountID: params.ExchangeAccountID,
		Exchange:          params.Exchange,
		AccountName:       params.AccountName,
		Symbol:            params.Symbol,
		PositionSide:      params.PositionSide,
		Quantity:          params.Quantity,
		NotionalUsdt:      params.NotionalUsdt,
		EntryPrice:        params.EntryPrice,
		MarkPrice:         params.MarkPrice,
		ObservedAt:        params.ObservedAt,
	}, nil
}

type hedgeAdapterStub struct {
	exchange.Adapter
	positions          []exchange.Position
	err                error
	order              exchange.OrderResult
	orderErr           error
	fetchOrderRequests []exchange.FetchOrderRequest
}

func (adapter *hedgeAdapterStub) FetchPositions(context.Context, exchange.FetchPositionsRequest) ([]exchange.Position, error) {
	return adapter.positions, adapter.err
}

func (adapter *hedgeAdapterStub) FetchOrder(_ context.Context, request exchange.FetchOrderRequest) (exchange.OrderResult, error) {
	adapter.fetchOrderRequests = append(adapter.fetchOrderRequests, request)
	return adapter.order, adapter.orderErr
}

func guardTestConfig() mgmt.HedgeConfig {
	return mgmt.HedgeConfig{
		ID:                4,
		AgentID:           1,
		ExchangeAccountID: 13,
		Symbol:            "BNBUSDT",
		TargetSymbol:      "BNB/USDT:USDT",
		ExchangeAccount: mgmt.ExchangeAccount{
			ID:       13,
			AgentID:  1,
			Exchange: "Binance",
			Name:     "paper",
			CCXTID:   "binanceusdm",
		},
	}
}

func TestMakeClientOrderIDUsesExchangeSafeFormat(t *testing.T) {
	exposure := coredb.ExposureSnapshot{
		ObservedAt: time.Date(2026, time.September, 1, 10, 51, 40, 266347000, time.UTC),
	}
	decision := hedge.Decision{
		Action:              hedge.ActionOpen,
		Symbol:              "BNB/USDT:USDT",
		TargetNotionalUSDT:  decimal.RequireFromString("-686.78"),
		CurrentNotionalUSDT: decimal.Zero,
		AdjustmentUSDT:      decimal.RequireFromString("-686.78"),
	}

	first := makeClientOrderID(4, exposure, decision)
	second := makeClientOrderID(4, exposure, decision)

	if first != second {
		t.Fatalf("同一笔对冲生成了不同的客户端订单号: %q != %q", first, second)
	}
	if len(first) != 28 {
		t.Fatalf("客户端订单号长度应为 28，实际为 %d: %q", len(first), first)
	}
	if !regexp.MustCompile(`^t-[0-9a-f]{26}$`).MatchString(first) {
		t.Fatalf("客户端订单号格式不正确: %q", first)
	}
}

func TestExistingOrderExecutionErrorPreservesFailedState(t *testing.T) {
	err := existingOrderExecutionError(coredb.OrderExecution{
		Status: exchange.OrderStatusFailed,
		ErrorMessage: sql.NullString{
			String: "bitget rejected order",
			Valid:  true,
		},
	})
	if err == nil || err.Error() != "existing order execution failed: bitget rejected order" {
		t.Fatalf("expected existing failed execution error, got %v", err)
	}

	if err := existingOrderExecutionError(coredb.OrderExecution{Status: exchange.OrderStatusFilled}); err != nil {
		t.Fatalf("filled execution should remain reusable: %v", err)
	}
}

func TestHedgeRatioAdjustmentForcesImmediateRebalanceReason(t *testing.T) {
	config := mgmt.HedgeConfig{
		TargetSymbol:     "BNB/USDT:USDT",
		TargetHedgeRatio: decimal.RequireFromString("0.9"),
		FirstTriggerUSDT: decimal.NewFromInt(600),
		RebalanceUSDT:    decimal.NewFromInt(200),
		ExitUSDT:         decimal.NewFromInt(500),
		MinOrderUSDT:     decimal.NewFromInt(10),
		Enabled:          true,
	}
	input := RunInput{Reason: hedge.ReasonHedgeRatioAdjustment}

	decision, err := hedge.NewCalculator().Calculate(
		calculationConfigForRun(config, input),
		hedge.Exposure{
			NetNotionalUSDT: decimal.NewFromInt(1000),
			MarkPrice:       decimal.NewFromInt(500),
		},
		hedge.Position{NotionalUSDT: decimal.NewFromInt(-800)},
	)
	if err != nil {
		t.Fatal(err)
	}
	applyRunReason(input, &decision)

	if decision.Action != hedge.ActionRebalance {
		t.Fatalf("expected forced rebalance, got %s", decision.Action)
	}
	if decision.Reason != "hedge_ratio_adjustment" {
		t.Fatalf("unexpected reason %q", decision.Reason)
	}
	if !decision.AdjustmentUSDT.Equal(decimal.NewFromInt(-100)) {
		t.Fatalf("unexpected adjustment %s", decision.AdjustmentUSDT)
	}
}

func TestHedgeRatioAdjustmentKeepsPositionFlipCloseReason(t *testing.T) {
	config := mgmt.HedgeConfig{
		TargetSymbol:     "BNB/USDT:USDT",
		TargetHedgeRatio: decimal.RequireFromString("0.7"),
		FirstTriggerUSDT: decimal.NewFromInt(600),
		RebalanceUSDT:    decimal.NewFromInt(200),
		ExitUSDT:         decimal.NewFromInt(500),
		MinOrderUSDT:     decimal.NewFromInt(10),
		Enabled:          true,
	}
	input := RunInput{Reason: hedge.ReasonHedgeRatioAdjustment}

	decision, err := hedge.NewCalculator().Calculate(
		calculationConfigForRun(config, input),
		hedge.Exposure{
			NetNotionalUSDT: decimal.RequireFromString("-2063.946"),
			MarkPrice:       decimal.RequireFromString("690"),
		},
		hedge.Position{
			Quantity:     decimal.RequireFromString("-1.387552728869565217"),
			NotionalUSDT: decimal.RequireFromString("-957.41138292"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	applyRunReason(input, &decision)

	if decision.Reason != hedge.ReasonPositionFlipClose || !decision.ReduceOnly || decision.Side != hedge.SideBuy {
		t.Fatalf("position flip close reason must be preserved: %+v", decision)
	}
}

func TestAutomaticRunGuardBlocksPendingCooldownAndPositionSyncWait(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 7, 13, 0, time.UTC)
	config := guardTestConfig()

	tests := []struct {
		name     string
		state    coredb.LatestOrderExecutionState
		position coredb.HedgePositionSnapshot
		want     string
	}{
		{
			name:  "submitted order",
			state: coredb.LatestOrderExecutionState{ConfigID: 4, Status: exchange.OrderStatusSubmitted, UpdatedAt: now.Add(-time.Second)},
			want:  reasonRecentOrderPending,
		},
		{
			name:  "filled order in cooldown",
			state: coredb.LatestOrderExecutionState{ConfigID: 4, Status: exchange.OrderStatusFilled, UpdatedAt: now.Add(-5 * time.Second)},
			want:  reasonRunCooldown,
		},
		{
			name:     "position snapshot predates fill",
			state:    coredb.LatestOrderExecutionState{ConfigID: 4, Status: exchange.OrderStatusFilled, UpdatedAt: now.Add(-20 * time.Second)},
			position: coredb.HedgePositionSnapshot{ObservedAt: now.Add(-30 * time.Second)},
			want:     reasonPositionSyncWait,
		},
		{
			name:     "position snapshot refreshed",
			state:    coredb.LatestOrderExecutionState{ConfigID: 4, Status: exchange.OrderStatusFilled, UpdatedAt: now.Add(-20 * time.Second)},
			position: coredb.HedgePositionSnapshot{ObservedAt: now.Add(-10 * time.Second)},
			want:     "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &hedgeStoreStub{state: test.state, position: test.position}
			service := NewHedgeService(store, nil, nil)
			service.now = func() time.Time { return now }
			service.runCooldown = 10 * time.Second

			got, err := service.automaticRunBlockReason(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("expected guard reason %q, got %q", test.want, got)
			}
		})
	}
}

func TestAutomaticRunGuardAllowsConfigWithoutExecution(t *testing.T) {
	store := &hedgeStoreStub{stateErr: sql.ErrNoRows}
	service := NewHedgeService(store, nil, nil)

	reason, err := service.automaticRunBlockReason(context.Background(), guardTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		t.Fatalf("expected no guard reason, got %q", reason)
	}
}

func TestAutomaticRunGuardReconcilesSubmittedOrderBeforeSkipping(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 7, 13, 0, time.UTC)
	store := &hedgeStoreStub{
		state: coredb.LatestOrderExecutionState{
			ConfigID:      4,
			OrderPlanID:   41,
			ExecutionID:   41,
			ClientOrderID: "h_test",
			Symbol:        "BNB/USDT:USDT",
			Side:          string(hedge.SideBuy),
			Status:        exchange.OrderStatusSubmitted,
			UpdatedAt:     now.Add(-time.Second),
		},
		updatedAt: now,
	}
	adapter := &hedgeAdapterStub{
		Adapter: exchange.NewSimulatedAdapter(),
		positions: []exchange.Position{{
			Symbol:       "BNB/USDT:USDT",
			PositionSide: "NET",
			Quantity:     decimal.RequireFromString("0.6"),
			NotionalUSDT: decimal.RequireFromString("412.2"),
		}},
		order: exchange.OrderResult{
			ExchangeOrderID: "exchange-41",
			Status:          exchange.OrderStatusFilled,
			FilledQuantity:  decimal.RequireFromString("0.6"),
			AvgPrice:        decimal.RequireFromString("687"),
		},
	}
	service := NewHedgeService(store, nil, adapter)
	service.now = func() time.Time { return now }

	reason, err := service.automaticRunBlockReason(context.Background(), guardTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if reason != reasonRunCooldown {
		t.Fatalf("expected reconciled fill to enter cooldown, got %q", reason)
	}
	if len(adapter.fetchOrderRequests) != 1 || len(store.executionUpdates) != 1 || len(store.planUpdates) != 1 {
		t.Fatalf("expected one reconciliation, requests=%d execution_updates=%d plan_updates=%d", len(adapter.fetchOrderRequests), len(store.executionUpdates), len(store.planUpdates))
	}
	if store.executionUpdates[0].Status != exchange.OrderStatusFilled || store.planUpdates[0].Status != exchange.OrderStatusFilled {
		t.Fatalf("reconciled order was not marked filled: %+v %+v", store.executionUpdates[0], store.planUpdates[0])
	}
	if len(store.upsertParams) != 1 || !store.upsertParams[0].Quantity.Equal(decimal.RequireFromString("0.6")) {
		t.Fatalf("reconciled fill did not refresh its position snapshot: %+v", store.upsertParams)
	}
}

func TestRefreshFilledPositionWritesZeroSnapshotAfterClose(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 5, 49, 0, time.UTC)
	store := &hedgeStoreStub{}
	adapter := &hedgeAdapterStub{Adapter: exchange.NewSimulatedAdapter()}
	service := NewHedgeService(store, nil, adapter)
	service.now = func() time.Time { return now }
	previous := &coredb.HedgePositionSnapshot{
		Quantity:     decimal.RequireFromString("-1.99"),
		NotionalUsdt: decimal.RequireFromString("-1368.48"),
		MarkPrice:    decimal.RequireFromString("687.68"),
	}
	result := RunResult{}

	service.refreshFilledPosition(
		context.Background(),
		guardTestConfig(),
		previous,
		hedge.Decision{Side: hedge.SideBuy, OrderQuantity: decimal.RequireFromString("1.99")},
		coredb.OrderExecution{ID: 39, Status: exchange.OrderStatusFilled, FilledQuantity: decimal.RequireFromString("1.99")},
		&result,
	)

	if len(store.upsertParams) != 1 {
		t.Fatalf("expected one zero position write, got %+v", store.upsertParams)
	}
	params := store.upsertParams[0]
	if !params.Quantity.IsZero() || !params.NotionalUsdt.IsZero() || !params.ObservedAt.Equal(now) {
		t.Fatalf("unexpected zero position snapshot %+v", params)
	}
	if result.Position == nil || !result.Position.Quantity.IsZero() {
		t.Fatalf("expected refreshed zero position in result, got %+v", result.Position)
	}
}

func TestRefreshFilledPositionDoesNotPersistStaleExchangePosition(t *testing.T) {
	store := &hedgeStoreStub{}
	adapter := &hedgeAdapterStub{
		Adapter: exchange.NewSimulatedAdapter(),
		positions: []exchange.Position{{
			Symbol:       "BNB/USDT:USDT",
			PositionSide: "SHORT",
			Quantity:     decimal.NewFromInt(2),
			NotionalUSDT: decimal.NewFromInt(1370),
		}},
	}
	service := NewHedgeService(store, nil, adapter)
	previous := &coredb.HedgePositionSnapshot{Quantity: decimal.NewFromInt(-2)}
	result := RunResult{}

	service.refreshFilledPosition(
		context.Background(),
		guardTestConfig(),
		previous,
		hedge.Decision{Side: hedge.SideBuy, OrderQuantity: decimal.RequireFromString("0.6")},
		coredb.OrderExecution{ID: 41, Status: exchange.OrderStatusFilled, FilledQuantity: decimal.RequireFromString("0.6")},
		&result,
	)

	if len(store.upsertParams) != 0 {
		t.Fatalf("stale position must not be marked as synchronized: %+v", store.upsertParams)
	}
	if store.auditEvents != 1 {
		t.Fatalf("expected one refresh warning audit, got %d", store.auditEvents)
	}
}
