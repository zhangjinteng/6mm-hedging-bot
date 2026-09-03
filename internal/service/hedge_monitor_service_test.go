package service

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

func TestCalculateHedgeMonitorStatuses(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	baseConfig := func() mgmt.HedgeMonitorConfig {
		return mgmt.HedgeMonitorConfig{
			GlobalEnabled: true,
			Config: mgmt.HedgeConfig{
				ID:                9,
				AgentID:           1,
				ExchangeAccountID: 3,
				Source:            "platform",
				Symbol:            "BTCUSDT",
				TargetSymbol:      "BTC/USDT:USDT",
				TargetHedgeRatio:  decimal.NewFromInt(1),
				FirstTriggerUSDT:  decimal.NewFromInt(5000),
				RebalanceUSDT:     decimal.NewFromInt(2000),
				ExitUSDT:          decimal.NewFromInt(1500),
				MinOrderUSDT:      decimal.NewFromInt(10),
				Enabled:           true,
				ExchangeAccount: mgmt.ExchangeAccount{
					ID:       3,
					AgentID:  1,
					Exchange: "Binance",
					Name:     "main",
					Status:   mgmt.ExchangeAccountStatusActive,
				},
			},
		}
	}
	baseExposure := coredb.ExposureSnapshot{
		AgentID:         1,
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetQuantity:     decimal.NewFromInt(1),
		NetNotionalUsdt: decimal.NewFromInt(10000),
		ObservedAt:      now.Add(-time.Second),
	}
	basePosition := coredb.HedgePositionSnapshot{
		AgentID:           1,
		ExchangeAccountID: 3,
		Symbol:            "BTC/USDT:USDT",
		PositionSide:      "NET",
		Quantity:          decimal.RequireFromString("-0.08"),
		NotionalUsdt:      decimal.NewFromInt(-8000),
		ObservedAt:        now.Add(-time.Second),
	}

	tests := []struct {
		name         string
		mutateConfig func(*mgmt.HedgeMonitorConfig)
		exposure     coredb.ExposureSnapshot
		hasExposure  bool
		position     coredb.HedgePositionSnapshot
		hasPosition  bool
		execution    coredb.ListLatestOrderExecutionStatesRow
		hasExecution bool
		want         string
	}{
		{name: "global off", mutateConfig: func(c *mgmt.HedgeMonitorConfig) { c.GlobalEnabled = false }, exposure: baseExposure, hasExposure: true, position: basePosition, hasPosition: true, want: MonitorSwitchGlobalOff},
		{name: "symbol off", mutateConfig: func(c *mgmt.HedgeMonitorConfig) { c.Config.Enabled = false }, exposure: baseExposure, hasExposure: true, position: basePosition, hasPosition: true, want: MonitorSwitchSymbolOff},
		{name: "account unavailable", mutateConfig: func(c *mgmt.HedgeMonitorConfig) { c.Config.ExchangeAccount.Status = mgmt.ExchangeAccountStatusDisabled }, exposure: baseExposure, hasExposure: true, position: basePosition, hasPosition: true, want: MonitorHealthAccountUnavailable},
		{name: "observing", exposure: coredb.ExposureSnapshot{AgentID: 1, NetNotionalUsdt: decimal.NewFromInt(1000), ObservedAt: now}, hasExposure: true, position: coredb.HedgePositionSnapshot{}, hasPosition: false, want: MonitorHealthObserving},
		{name: "execution failed", exposure: baseExposure, hasExposure: true, position: basePosition, hasPosition: true, execution: coredb.ListLatestOrderExecutionStatesRow{Status: "failed", ErrorMessage: "rejected"}, hasExecution: true, want: MonitorHealthExecutionFailed},
		{name: "execution failed before observing", exposure: baseExposure, hasExposure: true, position: coredb.HedgePositionSnapshot{}, hasPosition: false, execution: coredb.ListLatestOrderExecutionStatesRow{Status: "failed", ErrorMessage: "rejected"}, hasExecution: true, want: MonitorHealthExecutionFailed},
		{name: "open required before observing", exposure: baseExposure, hasExposure: true, position: coredb.HedgePositionSnapshot{}, hasPosition: false, want: MonitorActionOpenRequired},
		{name: "open required", exposure: baseExposure, hasExposure: true, position: coredb.HedgePositionSnapshot{AgentID: 1, ExchangeAccountID: 3, ObservedAt: now}, hasPosition: true, want: MonitorActionOpenRequired},
		{name: "rebalance required", exposure: baseExposure, hasExposure: true, position: coredb.HedgePositionSnapshot{AgentID: 1, ExchangeAccountID: 3, NotionalUsdt: decimal.NewFromInt(-5000), ObservedAt: now}, hasPosition: true, want: MonitorActionRebalanceRequired},
		{name: "exit required", exposure: coredb.ExposureSnapshot{AgentID: 1, NetNotionalUsdt: decimal.NewFromInt(1000), ObservedAt: now}, hasExposure: true, position: basePosition, hasPosition: true, want: MonitorActionExitRequired},
		{name: "balanced", exposure: baseExposure, hasExposure: true, position: coredb.HedgePositionSnapshot{AgentID: 1, ExchangeAccountID: 3, NotionalUsdt: decimal.NewFromInt(-9500), ObservedAt: now}, hasPosition: true, want: MonitorActionBalanced},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := baseConfig()
			if test.mutateConfig != nil {
				test.mutateConfig(&config)
			}
			params := calculateHedgeMonitorSnapshot(
				config,
				test.exposure,
				test.hasExposure,
				test.position,
				test.hasPosition,
				test.execution,
				test.hasExecution,
				now,
				time.Minute,
				time.Minute,
			)
			if params.Status != test.want {
				t.Fatalf("expected status %s, got %s (%s)", test.want, params.Status, params.StatusReason)
			}
		})
	}
}

func TestCalculateHedgeMonitorTargetAndAdjustment(t *testing.T) {
	now := time.Now().UTC()
	config := mgmt.HedgeMonitorConfig{GlobalEnabled: true, Config: mgmt.HedgeConfig{
		ID:                1,
		AgentID:           8,
		ExchangeAccountID: 2,
		Source:            "platform",
		Symbol:            "ETHUSDT",
		TargetSymbol:      "ETH/USDT:USDT",
		TargetHedgeRatio:  decimal.RequireFromString("0.8"),
		FirstTriggerUSDT:  decimal.NewFromInt(100),
		RebalanceUSDT:     decimal.NewFromInt(50),
		ExitUSDT:          decimal.NewFromInt(10),
		MinOrderUSDT:      decimal.NewFromInt(1),
		MaxNotionalUSDT:   decimal.NewFromInt(7000),
		Enabled:           true,
		ExchangeAccount:   mgmt.ExchangeAccount{ID: 2, AgentID: 8, Status: mgmt.ExchangeAccountStatusActive},
	}}
	params := calculateHedgeMonitorSnapshot(
		config,
		coredb.ExposureSnapshot{AgentID: 8, NetNotionalUsdt: decimal.NewFromInt(10000), ObservedAt: now}, true,
		coredb.HedgePositionSnapshot{AgentID: 8, ExchangeAccountID: 2, NotionalUsdt: decimal.NewFromInt(-5000), ObservedAt: now}, true,
		coredb.ListLatestOrderExecutionStatesRow{}, false,
		now, time.Minute, time.Minute,
	)
	if !params.TargetHedgeUsdt.Equal(decimal.NewFromInt(-7000)) {
		t.Fatalf("unexpected capped target %s", params.TargetHedgeUsdt)
	}
	if !params.AdjustmentUsdt.Equal(decimal.NewFromInt(-2000)) {
		t.Fatalf("unexpected adjustment %s", params.AdjustmentUsdt)
	}
}

func TestCalculateHedgeMonitorExitTargetsZero(t *testing.T) {
	now := time.Now().UTC()
	config := mgmt.HedgeMonitorConfig{GlobalEnabled: true, Config: mgmt.HedgeConfig{
		ID:                1,
		AgentID:           8,
		ExchangeAccountID: 2,
		Source:            "platform",
		Symbol:            "ETHUSDT",
		TargetSymbol:      "ETH/USDT:USDT",
		TargetHedgeRatio:  decimal.NewFromInt(1),
		FirstTriggerUSDT:  decimal.NewFromInt(100),
		RebalanceUSDT:     decimal.NewFromInt(50),
		ExitUSDT:          decimal.NewFromInt(10),
		MinOrderUSDT:      decimal.NewFromInt(1),
		Enabled:           true,
		ExchangeAccount:   mgmt.ExchangeAccount{ID: 2, AgentID: 8, Status: mgmt.ExchangeAccountStatusActive},
	}}
	params := calculateHedgeMonitorSnapshot(
		config,
		coredb.ExposureSnapshot{AgentID: 8, NetNotionalUsdt: decimal.NewFromInt(5), ObservedAt: now}, true,
		coredb.HedgePositionSnapshot{AgentID: 8, ExchangeAccountID: 2, NotionalUsdt: decimal.NewFromInt(-100), ObservedAt: now}, true,
		coredb.ListLatestOrderExecutionStatesRow{}, false,
		now, time.Minute, time.Minute,
	)
	if !params.TargetHedgeUsdt.IsZero() || !params.AdjustmentUsdt.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("exit must target zero hedge, got target=%s adjustment=%s", params.TargetHedgeUsdt, params.AdjustmentUsdt)
	}
	if params.Status != MonitorActionExitRequired {
		t.Fatalf("expected exit status, got %s", params.Status)
	}
}

func TestCalculateHedgeMonitorIgnoresExitBelowMinimumOrder(t *testing.T) {
	config := mgmt.HedgeConfig{ExitUSDT: decimal.NewFromInt(10), MinOrderUSDT: decimal.NewFromInt(5)}
	status, _ := monitorAction(config, decimal.NewFromInt(1), decimal.NewFromInt(-2), decimal.NewFromInt(2))
	if status != MonitorActionBalanced {
		t.Fatalf("expected balanced below minimum order, got %s", status)
	}
}

func TestSignedPositionNotionalUsesNetQuantitySign(t *testing.T) {
	position := coredb.HedgePositionSnapshot{
		PositionSide: "NET",
		Quantity:     decimal.RequireFromString("-0.5"),
		NotionalUsdt: decimal.NewFromInt(5000),
	}
	if got := signedPositionNotional(position); !got.Equal(decimal.NewFromInt(-5000)) {
		t.Fatalf("expected negative NET notional, got %s", got)
	}
}

func TestBuildHedgeMonitorListCountsSharedExposureOnce(t *testing.T) {
	items := []coredb.HedgeMonitorSnapshot{
		{
			AgentID:         7,
			ConfigID:        1,
			Source:          "platform",
			Symbol:          "BTCUSDT",
			NetNotionalUsdt: decimal.NewFromInt(10000),
			TargetHedgeUsdt: decimal.NewFromInt(-5000),
			ActualHedgeUsdt: decimal.NewFromInt(-4000),
			Status:          MonitorActionRebalanceRequired,
		},
		{
			AgentID:         7,
			ConfigID:        2,
			Source:          "PLATFORM",
			Symbol:          "btcusdt",
			NetNotionalUsdt: decimal.NewFromInt(10000),
			TargetHedgeUsdt: decimal.NewFromInt(-5000),
			ActualHedgeUsdt: decimal.NewFromInt(-5000),
			Status:          MonitorActionBalanced,
		},
	}

	result := buildHedgeMonitorList(items)
	if !result.Summary.NetExposureUSDT.Equal(decimal.NewFromInt(10000)) {
		t.Fatalf("shared exposure must be counted once, got %s", result.Summary.NetExposureUSDT)
	}
	if !result.Summary.TargetHedgeUSDT.Equal(decimal.NewFromInt(10000)) {
		t.Fatalf("target hedge must include both accounts, got %s", result.Summary.TargetHedgeUSDT)
	}
	if !result.Summary.ActualHedgeUSDT.Equal(decimal.NewFromInt(9000)) {
		t.Fatalf("actual hedge must include both accounts, got %s", result.Summary.ActualHedgeUSDT)
	}
	if len(result.Items) != 2 || result.Items[0].StatusLabel != "待再平衡" {
		t.Fatalf("unexpected monitor items %+v", result.Items)
	}
}
