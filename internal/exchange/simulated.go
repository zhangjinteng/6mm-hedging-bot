package exchange

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type SimulatedAdapter struct {
	mu           sync.RWMutex
	orders       map[string]OrderResult
	clientOrders map[string]string
}

func NewSimulatedAdapter() *SimulatedAdapter {
	return &SimulatedAdapter{
		orders:       map[string]OrderResult{},
		clientOrders: map[string]string{},
	}
}

func (a *SimulatedAdapter) FetchBalance(ctx context.Context, req FetchBalanceRequest) (BalanceResult, error) {
	if err := ctx.Err(); err != nil {
		return BalanceResult{}, err
	}

	assets := req.Assets
	if len(assets) == 0 {
		settle := strings.ToUpper(strings.TrimSpace(req.DefaultSettle))
		if settle == "" {
			settle = "USDT"
		}
		assets = []string{settle}
	}

	now := time.Now().UTC()
	items := make([]BalanceAsset, 0, len(assets))
	for _, asset := range assets {
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if asset == "" {
			return BalanceResult{}, errors.New("asset is required")
		}
		items = append(items, BalanceAsset{
			Asset:     asset,
			Free:      decimal.NewFromInt(1000000),
			Used:      decimal.Zero,
			Total:     decimal.NewFromInt(1000000),
			Debt:      decimal.Zero,
			UpdatedAt: now,
		})
	}

	return BalanceResult{
		Exchange:    req.Exchange,
		AccountName: req.AccountName,
		Assets:      items,
		Raw:         simulatedRaw(req.AccountConfig, map[string]any{"assets": assets}),
	}, nil
}

func (a *SimulatedAdapter) FetchPositions(ctx context.Context, req FetchPositionsRequest) ([]Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	allowed := map[string]struct{}{}
	for _, symbol := range req.Symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol != "" {
			allowed[symbol] = struct{}{}
		}
	}

	type aggregate struct {
		quantity decimal.Decimal
		notional decimal.Decimal
		mark     decimal.Decimal
	}
	positionsBySymbol := map[string]aggregate{}
	for _, order := range a.orders {
		if order.Status != OrderStatusFilled {
			continue
		}
		if req.Exchange != "" && order.Raw["exchange"] != req.Exchange {
			continue
		}
		if req.AccountName != "" && order.Raw["account_name"] != req.AccountName {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[order.Symbol]; !ok {
				continue
			}
		}

		sign := decimal.NewFromInt(1)
		if strings.EqualFold(order.Side, "SELL") || strings.EqualFold(order.Side, "sell") {
			sign = decimal.NewFromInt(-1)
		}
		current := positionsBySymbol[order.Symbol]
		current.quantity = current.quantity.Add(order.FilledQuantity.Mul(sign))
		current.notional = current.notional.Add(order.FilledQuantity.Mul(order.AvgPrice).Mul(sign))
		current.mark = order.AvgPrice
		positionsBySymbol[order.Symbol] = current
	}

	now := time.Now().UTC()
	positions := make([]Position, 0, len(positionsBySymbol))
	for symbol, item := range positionsBySymbol {
		if item.quantity.IsZero() {
			continue
		}
		positions = append(positions, Position{
			Exchange:          req.Exchange,
			AccountName:       req.AccountName,
			Symbol:            symbol,
			PositionSide:      "NET",
			Quantity:          item.quantity,
			NotionalUSDT:      item.notional,
			EntryPrice:        averageEntry(item.notional, item.quantity),
			MarkPrice:         item.mark,
			UnrealizedPnlUSDT: decimal.Zero,
			Leverage:          decimal.NewFromInt(1),
			MarginMode:        req.MarginMode,
			UpdatedAt:         now,
			Raw: simulatedRaw(req.AccountConfig, map[string]any{
				"symbol": symbol,
			}),
		})
	}
	return positions, nil
}

func (a *SimulatedAdapter) FetchTicker(ctx context.Context, req FetchTickerRequest) (Ticker, error) {
	if err := ctx.Err(); err != nil {
		return Ticker{}, err
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return Ticker{}, errors.New("symbol is required")
	}

	last := simulatedPrice(symbol)
	return Ticker{
		Exchange:  req.Exchange,
		Symbol:    symbol,
		Last:      last,
		Mark:      last,
		Bid:       last.Mul(decimal.RequireFromString("0.999")),
		Ask:       last.Mul(decimal.RequireFromString("1.001")),
		BidSize:   decimal.NewFromInt(10),
		AskSize:   decimal.NewFromInt(10),
		UpdatedAt: time.Now().UTC(),
		Raw: simulatedRaw(req.AccountConfig, map[string]any{
			"symbol": symbol,
		}),
	}, nil
}

func (a *SimulatedAdapter) FetchMarkets(ctx context.Context, req FetchMarketsRequest) ([]Market, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	exchangeName := strings.TrimSpace(req.Exchange)
	if exchangeName == "" {
		exchangeName = "Simulated"
	}
	ccxtID := strings.TrimSpace(req.CCXTID)
	if ccxtID == "" {
		ccxtID = strings.ToLower(exchangeName)
	}
	marketType := strings.ToLower(strings.TrimSpace(req.MarketType))
	if marketType == "" {
		marketType = "swap"
	}
	settle := strings.ToUpper(strings.TrimSpace(req.DefaultSettle))
	if settle == "" {
		settle = "USDT"
	}

	now := time.Now().UTC()
	symbols := []struct {
		symbol string
		base   string
	}{
		{symbol: "BTC/" + settle + ":" + settle, base: "BTC"},
		{symbol: "ETH/" + settle + ":" + settle, base: "ETH"},
		{symbol: "SOL/" + settle + ":" + settle, base: "SOL"},
	}
	markets := make([]Market, 0, len(symbols))
	for _, item := range symbols {
		markets = append(markets, Market{
			Exchange:        exchangeName,
			CCXTID:          ccxtID,
			Symbol:          item.symbol,
			Base:            item.base,
			Quote:           settle,
			Settle:          settle,
			MarketType:      marketType,
			Active:          true,
			Contract:        marketType == "swap" || marketType == "future",
			Linear:          settle == "USDT" || settle == "USDC",
			Swap:            marketType == "swap",
			Future:          marketType == "future",
			Spot:            marketType == "spot",
			PricePrecision:  decimal.RequireFromString("0.1"),
			AmountPrecision: decimal.RequireFromString("0.001"),
			MinAmount:       decimal.RequireFromString("0.001"),
			MinCost:         decimal.RequireFromString("5"),
			ContractSize:    decimal.NewFromInt(1),
			CreatedAt:       now,
			Raw: simulatedRaw(req.AccountConfig, map[string]any{
				"symbol": item.symbol,
			}),
		})
	}
	return markets, nil
}

func (a *SimulatedAdapter) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (OrderResult, error) {
	if err := ctx.Err(); err != nil {
		return OrderResult{}, err
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return OrderResult{}, errors.New("symbol is required")
	}
	if !req.Quantity.GreaterThan(decimal.Zero) {
		return OrderResult{}, errors.New("quantity must be greater than zero")
	}
	if !strings.EqualFold(req.OrderType, "market") && !req.Price.GreaterThan(decimal.Zero) {
		return OrderResult{}, errors.New("price must be greater than zero for non-market order")
	}

	sum := sha256.Sum256([]byte(req.ClientOrderID + time.Now().UTC().Format(time.RFC3339Nano)))
	orderID := "sim_" + hex.EncodeToString(sum[:])[:24]
	status := OrderStatusFilled
	if rawStatus, ok := req.ExchangeOrderParams["simulate_status"].(string); ok && strings.TrimSpace(rawStatus) != "" {
		status = strings.ToLower(strings.TrimSpace(rawStatus))
	}
	filledQuantity := req.Quantity
	avgPrice := req.Price
	if status != OrderStatusFilled {
		filledQuantity = decimal.Zero
		avgPrice = decimal.Zero
	}

	result := OrderResult{
		ExchangeOrderID: orderID,
		ClientOrderID:   req.ClientOrderID,
		Status:          status,
		Symbol:          req.Symbol,
		Side:            strings.ToUpper(req.Side),
		OrderType:       strings.ToUpper(req.OrderType),
		Quantity:        req.Quantity,
		Price:           req.Price,
		ReduceOnly:      req.ReduceOnly,
		FilledQuantity:  filledQuantity,
		AvgPrice:        avgPrice,
		FeeUSDT:         decimal.Zero,
		UpdatedAt:       time.Now().UTC(),
		Raw: map[string]any{
			"simulated":       true,
			"client_order_id": req.ClientOrderID,
			"exchange":        req.Exchange,
			"account_name":    req.AccountName,
			"ccxt_id":         req.CCXTID,
			"market_type":     req.MarketType,
			"sandbox":         req.Sandbox,
			"symbol":          req.Symbol,
		},
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureState()
	a.orders[result.ExchangeOrderID] = result
	if result.ClientOrderID != "" {
		a.clientOrders[result.ClientOrderID] = result.ExchangeOrderID
	}
	return result, nil
}

func (a *SimulatedAdapter) FetchOrder(ctx context.Context, req FetchOrderRequest) (OrderResult, error) {
	if err := ctx.Err(); err != nil {
		return OrderResult{}, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.findOrderLocked(req.OrderID, req.ClientOrderID)
}

func (a *SimulatedAdapter) CancelOrder(ctx context.Context, req CancelOrderRequest) (OrderResult, error) {
	if err := ctx.Err(); err != nil {
		return OrderResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.findOrderLocked(req.OrderID, req.ClientOrderID)
	if err != nil {
		return OrderResult{}, err
	}
	if result.Status == OrderStatusFilled || result.Status == OrderStatusCanceled || result.Status == OrderStatusFailed {
		return OrderResult{}, fmt.Errorf("%s order cannot be canceled: %w", result.Status, ErrOrderAlreadyFinal)
	}

	result.Status = OrderStatusCanceled
	result.UpdatedAt = time.Now().UTC()
	result.Raw = copyRaw(result.Raw)
	result.Raw["canceled"] = true
	a.orders[result.ExchangeOrderID] = result
	if result.ClientOrderID != "" {
		a.clientOrders[result.ClientOrderID] = result.ExchangeOrderID
	}
	return result, nil
}

func (a *SimulatedAdapter) SetLeverage(ctx context.Context, req SetLeverageRequest) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return CommandResult{}, errors.New("symbol is required")
	}
	if req.Leverage <= 0 {
		return CommandResult{}, errors.New("leverage must be greater than zero")
	}
	return CommandResult{
		Success: true,
		Raw: simulatedRaw(req.AccountConfig, map[string]any{
			"symbol":   req.Symbol,
			"leverage": req.Leverage,
		}),
	}, nil
}

func (a *SimulatedAdapter) SetMarginMode(ctx context.Context, req SetMarginModeRequest) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(req.MarginMode))
	if strings.TrimSpace(req.Symbol) == "" {
		return CommandResult{}, errors.New("symbol is required")
	}
	if mode != "cross" && mode != "isolated" {
		return CommandResult{}, errors.New("margin_mode must be cross or isolated")
	}
	return CommandResult{
		Success: true,
		Raw: simulatedRaw(req.AccountConfig, map[string]any{
			"symbol":      req.Symbol,
			"margin_mode": mode,
		}),
	}, nil
}

func (a *SimulatedAdapter) ensureState() {
	if a.orders == nil {
		a.orders = map[string]OrderResult{}
	}
	if a.clientOrders == nil {
		a.clientOrders = map[string]string{}
	}
}

func (a *SimulatedAdapter) findOrderLocked(orderID, clientOrderID string) (OrderResult, error) {
	a.ensureState()
	if orderID != "" {
		if result, ok := a.orders[orderID]; ok {
			return result, nil
		}
	}
	if clientOrderID != "" {
		if mappedOrderID, ok := a.clientOrders[clientOrderID]; ok {
			if result, ok := a.orders[mappedOrderID]; ok {
				return result, nil
			}
		}
	}
	return OrderResult{}, ErrOrderNotFound
}

func simulatedRaw(account AccountConfig, values map[string]any) map[string]any {
	raw := map[string]any{
		"simulated":    true,
		"exchange":     account.Exchange,
		"account_name": account.AccountName,
		"ccxt_id":      account.CCXTID,
		"market_type":  account.MarketType,
		"sandbox":      account.Sandbox,
	}
	for key, value := range values {
		raw[key] = value
	}
	return raw
}

func simulatedPrice(symbol string) decimal.Decimal {
	symbol = strings.ToUpper(symbol)
	switch {
	case strings.HasPrefix(symbol, "BTC"):
		return decimal.NewFromInt(50000)
	case strings.HasPrefix(symbol, "ETH"):
		return decimal.NewFromInt(4000)
	case strings.HasPrefix(symbol, "SOL"):
		return decimal.NewFromInt(150)
	default:
		return decimal.NewFromInt(1)
	}
}

func averageEntry(notional, quantity decimal.Decimal) decimal.Decimal {
	if quantity.IsZero() {
		return decimal.Zero
	}
	return notional.Div(quantity)
}

func copyRaw(raw map[string]any) map[string]any {
	copied := make(map[string]any, len(raw))
	for key, value := range raw {
		copied[key] = value
	}
	return copied
}
