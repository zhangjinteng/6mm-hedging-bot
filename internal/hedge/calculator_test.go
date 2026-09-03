package hedge

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCalculatorOpenShortHedge(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(10000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionOpen {
		t.Fatalf("expected open, got %s", decision.Action)
	}
	if decision.Reason != "first_trigger" {
		t.Fatalf("unexpected open reason %q", decision.Reason)
	}
	if decision.Side != SideSell {
		t.Fatalf("expected sell, got %s", decision.Side)
	}
	if !decision.OrderQuantity.Equal(decimal.RequireFromString("0.2")) {
		t.Fatalf("unexpected quantity %s", decision.OrderQuantity)
	}
	if !decision.LimitPrice.Equal(decimal.RequireFromString("49850")) {
		t.Fatalf("unexpected limit price %s", decision.LimitPrice)
	}
}

func TestCalculatorHoldBeforeFirstTrigger(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(3000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionHold {
		t.Fatalf("expected hold, got %s", decision.Action)
	}
}

func TestCalculatorRebalance(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(14000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{
		NotionalUSDT: decimal.NewFromInt(-10000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionRebalance {
		t.Fatalf("expected rebalance, got %s", decision.Action)
	}
	if decision.Reason != "rebalance" {
		t.Fatalf("unexpected rebalance reason %q", decision.Reason)
	}
	if decision.Side != SideSell {
		t.Fatalf("expected sell, got %s", decision.Side)
	}
	if !decision.AdjustmentUSDT.Equal(decimal.NewFromInt(-4000)) {
		t.Fatalf("unexpected adjustment %s", decision.AdjustmentUSDT)
	}
}

func TestCalculatorExitWithReduceOnly(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(1000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{
		NotionalUSDT: decimal.NewFromInt(-8000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionExit {
		t.Fatalf("expected exit, got %s", decision.Action)
	}
	if decision.Reason != "exit_hedge" {
		t.Fatalf("unexpected exit reason %q", decision.Reason)
	}
	if decision.Side != SideBuy {
		t.Fatalf("expected buy, got %s", decision.Side)
	}
	if !decision.ReduceOnly {
		t.Fatal("expected reduce only")
	}
}

func TestCalculatorCloseShortPosition(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.ClosePosition(baseConfig(), Position{
		Symbol:       "BTCUSDT",
		Quantity:     decimal.RequireFromString("-0.16"),
		NotionalUSDT: decimal.NewFromInt(-8000),
		MarkPrice:    decimal.NewFromInt(50000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionExit {
		t.Fatalf("expected exit, got %s", decision.Action)
	}
	if decision.Side != SideBuy {
		t.Fatalf("expected buy, got %s", decision.Side)
	}
	if !decision.ReduceOnly {
		t.Fatal("expected reduce only")
	}
	if decision.Reason != "manual_close" {
		t.Fatalf("unexpected close reason %q", decision.Reason)
	}
	if !decision.OrderQuantity.Equal(decimal.RequireFromString("0.16")) {
		t.Fatalf("unexpected quantity %s", decision.OrderQuantity)
	}
	if !decision.LimitPrice.Equal(decimal.RequireFromString("50150")) {
		t.Fatalf("unexpected limit price %s", decision.LimitPrice)
	}
}

func TestCalculatorCloseLongPosition(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.ClosePosition(baseConfig(), Position{
		Symbol:       "BTCUSDT",
		Quantity:     decimal.RequireFromString("0.2"),
		NotionalUSDT: decimal.NewFromInt(10000),
		MarkPrice:    decimal.NewFromInt(50000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionExit {
		t.Fatalf("expected exit, got %s", decision.Action)
	}
	if decision.Side != SideSell {
		t.Fatalf("expected sell, got %s", decision.Side)
	}
	if !decision.ReduceOnly {
		t.Fatal("expected reduce only")
	}
	if !decision.LimitPrice.Equal(decimal.RequireFromString("49850")) {
		t.Fatalf("unexpected limit price %s", decision.LimitPrice)
	}
}

func TestCalculatorCloseEmptyPositionHolds(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.ClosePosition(baseConfig(), Position{
		Symbol:    "BTCUSDT",
		MarkPrice: decimal.NewFromInt(50000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionHold {
		t.Fatalf("expected hold, got %s", decision.Action)
	}
	if decision.ReduceOnly {
		t.Fatal("empty position should not create reduce-only order")
	}
}

func TestCalculatorClosesShortBeforeOpeningLong(t *testing.T) {
	calc := NewCalculator()

	decision, err := calc.Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(-10000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{
		Quantity:     decimal.RequireFromString("-0.16"),
		NotionalUSDT: decimal.NewFromInt(-8000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionRebalance {
		t.Fatalf("expected rebalance close leg, got %s", decision.Action)
	}
	if decision.Reason != ReasonPositionFlipClose {
		t.Fatalf("unexpected reason %q", decision.Reason)
	}
	if decision.Side != SideBuy || !decision.ReduceOnly {
		t.Fatalf("expected reduce-only buy, got side=%s reduce_only=%t", decision.Side, decision.ReduceOnly)
	}
	if !decision.AdjustmentUSDT.Equal(decimal.NewFromInt(8000)) {
		t.Fatalf("close leg must only remove the current short, got %s", decision.AdjustmentUSDT)
	}
	if !decision.OrderQuantity.Equal(decimal.RequireFromString("0.16")) {
		t.Fatalf("close leg must use the exact position quantity, got %s", decision.OrderQuantity)
	}
	if !decision.LimitPrice.Equal(decimal.RequireFromString("50150")) {
		t.Fatalf("unexpected close limit price %s", decision.LimitPrice)
	}
}

func TestCalculatorOpensLongAfterPositionFlipClose(t *testing.T) {
	calc := NewCalculator()
	exposure := Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(-10000),
		MarkPrice:       decimal.NewFromInt(50000),
	}

	closeDecision, err := calc.Calculate(baseConfig(), exposure, Position{
		Quantity:     decimal.RequireFromString("-0.16"),
		NotionalUSDT: decimal.NewFromInt(-8000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if closeDecision.Reason != ReasonPositionFlipClose {
		t.Fatalf("expected position flip close, got %+v", closeDecision)
	}

	openDecision, err := calc.Calculate(baseConfig(), exposure, Position{})
	if err != nil {
		t.Fatal(err)
	}
	if openDecision.Action != ActionOpen || openDecision.Side != SideBuy || openDecision.ReduceOnly {
		t.Fatalf("expected normal long open after close, got %+v", openDecision)
	}
	if openDecision.Reason != ReasonFirstTrigger || !openDecision.OrderQuantity.Equal(decimal.RequireFromString("0.2")) {
		t.Fatalf("unexpected second leg %+v", openDecision)
	}
}

func TestCalculatorClosesLongBeforeOpeningShort(t *testing.T) {
	decision, err := NewCalculator().Calculate(baseConfig(), Exposure{
		Source:          "platform",
		Symbol:          "BTCUSDT",
		NetNotionalUSDT: decimal.NewFromInt(10000),
		MarkPrice:       decimal.NewFromInt(50000),
	}, Position{
		Quantity:     decimal.RequireFromString("0.16"),
		NotionalUSDT: decimal.NewFromInt(8000),
	})

	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionRebalance || decision.Reason != ReasonPositionFlipClose {
		t.Fatalf("expected position flip close, got %+v", decision)
	}
	if decision.Side != SideSell || !decision.ReduceOnly {
		t.Fatalf("expected reduce-only sell, got side=%s reduce_only=%t", decision.Side, decision.ReduceOnly)
	}
	if !decision.AdjustmentUSDT.Equal(decimal.NewFromInt(-8000)) || !decision.OrderQuantity.Equal(decimal.RequireFromString("0.16")) {
		t.Fatalf("unexpected close leg %+v", decision)
	}
}

func baseConfig() Config {
	return Config{
		ID:               1,
		Source:           "platform",
		Symbol:           "BTCUSDT",
		TargetSymbol:     "BTCUSDT",
		TargetHedgeRatio: decimal.NewFromInt(1),
		FirstTriggerUSDT: decimal.NewFromInt(5000),
		RebalanceUSDT:    decimal.NewFromInt(2000),
		ExitUSDT:         decimal.NewFromInt(1500),
		MaxSlippageBps:   30,
		MinOrderUSDT:     decimal.NewFromInt(10),
		Enabled:          true,
		DryRun:           true,
	}
}
