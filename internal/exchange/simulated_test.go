package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestSimulatedAdapterOrderAndPositionLifecycle(t *testing.T) {
	ctx := context.Background()
	adapter := NewSimulatedAdapter()
	account := simulatedTestAccount()

	order, err := adapter.PlaceOrder(ctx, PlaceOrderRequest{
		ClientOrderID: "test-order-1",
		Exchange:      account.Exchange,
		AccountName:   account.AccountName,
		CCXTID:        account.CCXTID,
		MarketType:    account.MarketType,
		DefaultSettle: account.DefaultSettle,
		Symbol:        "BTC/USDT:USDT",
		Side:          "sell",
		OrderType:     "limit",
		Quantity:      decimal.RequireFromString("0.2"),
		Price:         decimal.NewFromInt(50000),
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	if order.ExchangeOrderID == "" {
		t.Fatal("expected exchange order id")
	}
	if order.Status != OrderStatusFilled {
		t.Fatalf("unexpected order status %s", order.Status)
	}
	if !order.FilledQuantity.Equal(decimal.RequireFromString("0.2")) {
		t.Fatalf("unexpected filled quantity %s", order.FilledQuantity)
	}

	fetched, err := adapter.FetchOrder(ctx, FetchOrderRequest{
		AccountConfig: account,
		ClientOrderID: "test-order-1",
	})
	if err != nil {
		t.Fatalf("fetch order: %v", err)
	}
	if fetched.ExchangeOrderID != order.ExchangeOrderID {
		t.Fatalf("unexpected fetched order id %s", fetched.ExchangeOrderID)
	}

	positions, err := adapter.FetchPositions(ctx, FetchPositionsRequest{
		AccountConfig: account,
		Symbols:       []string{"BTC/USDT:USDT"},
	})
	if err != nil {
		t.Fatalf("fetch positions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected one position, got %d", len(positions))
	}
	position := positions[0]
	if !position.Quantity.Equal(decimal.RequireFromString("-0.2")) {
		t.Fatalf("unexpected position quantity %s", position.Quantity)
	}
	if !position.NotionalUSDT.Equal(decimal.RequireFromString("-10000")) {
		t.Fatalf("unexpected position notional %s", position.NotionalUSDT)
	}
	if !position.EntryPrice.Equal(decimal.NewFromInt(50000)) {
		t.Fatalf("unexpected entry price %s", position.EntryPrice)
	}
}

func TestSimulatedAdapterCancelOrder(t *testing.T) {
	ctx := context.Background()
	adapter := NewSimulatedAdapter()
	account := simulatedTestAccount()

	order, err := adapter.PlaceOrder(ctx, PlaceOrderRequest{
		ClientOrderID:       "test-order-2",
		Exchange:            account.Exchange,
		AccountName:         account.AccountName,
		CCXTID:              account.CCXTID,
		MarketType:          account.MarketType,
		DefaultSettle:       account.DefaultSettle,
		Symbol:              "ETH/USDT:USDT",
		Side:                "buy",
		OrderType:           "limit",
		Quantity:            decimal.RequireFromString("1.5"),
		Price:               decimal.NewFromInt(4000),
		ExchangeOrderParams: map[string]any{"simulate_status": OrderStatusSubmitted},
	})
	if err != nil {
		t.Fatalf("place submitted order: %v", err)
	}

	canceled, err := adapter.CancelOrder(ctx, CancelOrderRequest{
		AccountConfig: account,
		OrderID:       order.ExchangeOrderID,
	})
	if err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	if canceled.Status != OrderStatusCanceled {
		t.Fatalf("unexpected canceled status %s", canceled.Status)
	}

	_, err = adapter.CancelOrder(ctx, CancelOrderRequest{
		AccountConfig: account,
		OrderID:       order.ExchangeOrderID,
	})
	if !errors.Is(err, ErrOrderAlreadyFinal) {
		t.Fatalf("expected ErrOrderAlreadyFinal, got %v", err)
	}

	_, err = adapter.CancelOrder(ctx, CancelOrderRequest{
		AccountConfig: account,
		OrderID:       "missing",
	})
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
}

func TestSimulatedAdapterMarketAndAccountCalls(t *testing.T) {
	ctx := context.Background()
	adapter := NewSimulatedAdapter()
	account := simulatedTestAccount()

	balance, err := adapter.FetchBalance(ctx, FetchBalanceRequest{
		AccountConfig: account,
		Assets:        []string{"usdt", "btc"},
	})
	if err != nil {
		t.Fatalf("fetch balance: %v", err)
	}
	if len(balance.Assets) != 2 {
		t.Fatalf("expected two assets, got %d", len(balance.Assets))
	}
	if balance.Assets[0].Asset != "USDT" || !balance.Assets[0].Free.Equal(decimal.NewFromInt(1000000)) {
		t.Fatalf("unexpected first asset %+v", balance.Assets[0])
	}

	ticker, err := adapter.FetchTicker(ctx, FetchTickerRequest{
		AccountConfig: account,
		Symbol:        "ETH/USDT:USDT",
	})
	if err != nil {
		t.Fatalf("fetch ticker: %v", err)
	}
	if !ticker.Last.Equal(decimal.NewFromInt(4000)) {
		t.Fatalf("unexpected ticker last %s", ticker.Last)
	}
	if !ticker.Ask.GreaterThan(ticker.Bid) {
		t.Fatalf("expected ask greater than bid, got ask=%s bid=%s", ticker.Ask, ticker.Bid)
	}

	leverage, err := adapter.SetLeverage(ctx, SetLeverageRequest{
		AccountConfig: account,
		Symbol:        "BTC/USDT:USDT",
		Leverage:      20,
	})
	if err != nil {
		t.Fatalf("set leverage: %v", err)
	}
	if !leverage.Success {
		t.Fatal("expected set leverage success")
	}

	marginMode, err := adapter.SetMarginMode(ctx, SetMarginModeRequest{
		AccountConfig: account,
		Symbol:        "BTC/USDT:USDT",
		MarginMode:    "isolated",
	})
	if err != nil {
		t.Fatalf("set margin mode: %v", err)
	}
	if !marginMode.Success {
		t.Fatal("expected set margin mode success")
	}
}

func simulatedTestAccount() AccountConfig {
	return AccountConfig{
		Exchange:      "Binance",
		AccountName:   "main",
		CCXTID:        "binanceusdm",
		MarketType:    "swap",
		DefaultSettle: "USDT",
		MarginMode:    "cross",
	}
}
