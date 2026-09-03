package service

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

type fakePositionSyncRepository struct {
	configs []mgmt.HedgeMonitorConfig
}

func (repository *fakePositionSyncRepository) ListHedgeMonitorConfigs(context.Context) ([]mgmt.HedgeMonitorConfig, error) {
	return repository.configs, nil
}

type fakePositionSnapshotWriter struct {
	params []coredb.UpsertHedgePositionSnapshotParams
}

func (writer *fakePositionSnapshotWriter) UpsertHedgePositionSnapshot(_ context.Context, params coredb.UpsertHedgePositionSnapshotParams) (coredb.HedgePositionSnapshot, error) {
	writer.params = append(writer.params, params)
	return coredb.HedgePositionSnapshot{}, nil
}

type positionSyncAdapter struct {
	exchange.Adapter
	requests  []exchange.FetchPositionsRequest
	positions []exchange.Position
	err       error
}

func (adapter *positionSyncAdapter) FetchPositions(_ context.Context, request exchange.FetchPositionsRequest) ([]exchange.Position, error) {
	adapter.requests = append(adapter.requests, request)
	return adapter.positions, adapter.err
}

func positionMonitorConfig(enabled bool) mgmt.HedgeMonitorConfig {
	account := mgmt.ExchangeAccount{
		ID:         7,
		AgentID:    3,
		Exchange:   "Binance",
		Name:       "main",
		CCXTID:     "binanceusdm",
		MarketType: "swap",
		Status:     mgmt.ExchangeAccountStatusActive,
	}
	return mgmt.HedgeMonitorConfig{GlobalEnabled: true, Config: mgmt.HedgeConfig{
		ID:                11,
		AgentID:           3,
		ExchangeAccountID: account.ID,
		ExchangeAccount:   account,
		Symbol:            "BTCUSDT",
		TargetSymbol:      "BTC/USDT:USDT",
		Enabled:           enabled,
	}}
}

func TestPositionSyncWritesFreshZeroForMissingPosition(t *testing.T) {
	config := positionMonitorConfig(false)
	adapter := &positionSyncAdapter{Adapter: exchange.NewSimulatedAdapter()}
	writer := &fakePositionSnapshotWriter{}
	service := NewPositionSyncService(&fakePositionSyncRepository{configs: []mgmt.HedgeMonitorConfig{config}}, writer, adapter, nil)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.requests) != 1 || len(adapter.requests[0].Symbols) != 1 {
		t.Fatalf("unexpected position requests %+v", adapter.requests)
	}
	if len(writer.params) != 1 {
		t.Fatalf("expected one zero snapshot, got %+v", writer.params)
	}
	params := writer.params[0]
	if params.AgentID != 3 || params.ExchangeAccountID != 7 || !params.Quantity.IsZero() || !params.NotionalUsdt.IsZero() || !params.ObservedAt.Equal(now) {
		t.Fatalf("unexpected zero snapshot %+v", params)
	}
	if result.Accounts != 1 || result.Positions != 1 {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestPositionSyncNormalizesShortPosition(t *testing.T) {
	config := positionMonitorConfig(true)
	adapter := &positionSyncAdapter{
		Adapter: exchange.NewSimulatedAdapter(),
		positions: []exchange.Position{{
			Symbol:       "BTC/USDT:USDT",
			PositionSide: "SHORT",
			Quantity:     decimal.RequireFromString("0.5"),
			NotionalUSDT: decimal.NewFromInt(30000),
			MarkPrice:    decimal.NewFromInt(60000),
		}},
	}
	writer := &fakePositionSnapshotWriter{}
	service := NewPositionSyncService(&fakePositionSyncRepository{configs: []mgmt.HedgeMonitorConfig{config}}, writer, adapter, nil)

	if _, err := service.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.params) != 1 {
		t.Fatalf("unexpected writes %+v", writer.params)
	}
	if !writer.params[0].Quantity.Equal(decimal.RequireFromString("-0.5")) || !writer.params[0].NotionalUsdt.Equal(decimal.NewFromInt(-30000)) {
		t.Fatalf("short position was not normalized %+v", writer.params[0])
	}
}

func TestNormalizeExchangePositionValuesSignsUnsignedShortNotional(t *testing.T) {
	quantity, notional := normalizeExchangePositionValues(exchange.Position{
		PositionSide: "SHORT",
		Quantity:     decimal.RequireFromString("1.99"),
		NotionalUSDT: decimal.RequireFromString("1369.9558"),
	})

	if !quantity.Equal(decimal.RequireFromString("-1.99")) {
		t.Fatalf("expected negative short quantity, got %s", quantity)
	}
	if !notional.Equal(decimal.RequireFromString("-1369.9558")) {
		t.Fatalf("expected negative short notional, got %s", notional)
	}
}
