//go:build ccxt

// 真实 CCXT 适配器默认只在 ccxt 编译标签下启用，避免普通本地开发误触真实交易所。
package exchange

import (
	"context"
	"errors"

	sixmmccxt "github.com/zhangjinteng/6mm-ccxt"
)

// CCXTAdapter 是业务层 Adapter 与 6mm-ccxt 扩展包之间的桥接层。
type CCXTAdapter struct {
	client sixmmccxt.Client
}

func init() {
	RegisterAdapter("ccxt", func(options AdapterOptions) (Adapter, error) {
		return NewCCXTAdapter(options)
	})
}

func NewCCXTAdapter(options AdapterOptions) (CCXTAdapter, error) {
	client, err := sixmmccxt.NewClient(sixmmccxt.Options{
		ExchangeEnv: options.ExchangeEnv,
	})
	if err != nil {
		return CCXTAdapter{}, err
	}
	return CCXTAdapter{client: client}, nil
}

func (adapter CCXTAdapter) FetchBalance(ctx context.Context, req FetchBalanceRequest) (BalanceResult, error) {
	result, err := adapter.client.FetchBalance(ctx, sixmmccxt.FetchBalanceRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Assets:        req.Assets,
		Params:        req.Params,
	})
	if err != nil {
		return BalanceResult{}, fromSixError(err)
	}
	return fromSixBalanceResult(result), nil
}

func (adapter CCXTAdapter) FetchPositions(ctx context.Context, req FetchPositionsRequest) ([]Position, error) {
	results, err := adapter.client.FetchPositions(ctx, sixmmccxt.FetchPositionsRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Symbols:       req.Symbols,
		Params:        req.Params,
	})
	if err != nil {
		return nil, fromSixError(err)
	}
	return fromSixPositions(results), nil
}

func (adapter CCXTAdapter) FetchTicker(ctx context.Context, req FetchTickerRequest) (Ticker, error) {
	result, err := adapter.client.FetchTicker(ctx, sixmmccxt.FetchTickerRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Symbol:        req.Symbol,
		Params:        req.Params,
	})
	if err != nil {
		return Ticker{}, fromSixError(err)
	}
	return fromSixTicker(result), nil
}

func (adapter CCXTAdapter) FetchMarkets(ctx context.Context, req FetchMarketsRequest) ([]Market, error) {
	results, err := adapter.client.FetchMarkets(ctx, sixmmccxt.FetchMarketsRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Params:        req.Params,
	})
	if err != nil {
		return nil, fromSixError(err)
	}
	return fromSixMarkets(results), nil
}

func (adapter CCXTAdapter) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (OrderResult, error) {
	result, err := adapter.client.PlaceOrder(ctx, sixmmccxt.PlaceOrderRequest{
		ClientOrderID:       req.ClientOrderID,
		Exchange:            req.Exchange,
		AccountName:         req.AccountName,
		CCXTID:              req.CCXTID,
		MarketType:          req.MarketType,
		Sandbox:             req.Sandbox,
		DefaultSettle:       req.DefaultSettle,
		AccountType:         req.AccountType,
		ProductType:         req.ProductType,
		Category:            req.Category,
		PositionMode:        req.PositionMode,
		MarginMode:          req.MarginMode,
		RecvWindowMS:        req.RecvWindowMS,
		RateLimitMS:         req.RateLimitMS,
		APIKey:              req.APIKey,
		APISecret:           req.APISecret,
		Passphrase:          req.Passphrase,
		Symbol:              req.Symbol,
		Side:                req.Side,
		PositionSide:        req.PositionSide,
		OrderType:           req.OrderType,
		Quantity:            req.Quantity,
		Price:               req.Price,
		ReduceOnly:          req.ReduceOnly,
		ExchangeOrderParams: req.ExchangeOrderParams,
	})
	if err != nil {
		return OrderResult{}, fromSixError(err)
	}
	return fromSixOrderResult(result), nil
}

func (adapter CCXTAdapter) FetchOrder(ctx context.Context, req FetchOrderRequest) (OrderResult, error) {
	result, err := adapter.client.FetchOrder(ctx, sixmmccxt.FetchOrderRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		OrderID:       req.OrderID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Params:        req.Params,
	})
	if err != nil {
		return OrderResult{}, fromSixError(err)
	}
	return fromSixOrderResult(result), nil
}

func (adapter CCXTAdapter) CancelOrder(ctx context.Context, req CancelOrderRequest) (OrderResult, error) {
	result, err := adapter.client.CancelOrder(ctx, sixmmccxt.CancelOrderRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		OrderID:       req.OrderID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Params:        req.Params,
	})
	if err != nil {
		return OrderResult{}, fromSixError(err)
	}
	return fromSixOrderResult(result), nil
}

func (adapter CCXTAdapter) SetLeverage(ctx context.Context, req SetLeverageRequest) (CommandResult, error) {
	result, err := adapter.client.SetLeverage(ctx, sixmmccxt.SetLeverageRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Symbol:        req.Symbol,
		Leverage:      req.Leverage,
		Params:        req.Params,
	})
	if err != nil {
		return CommandResult{}, fromSixError(err)
	}
	return fromSixCommandResult(result), nil
}

func (adapter CCXTAdapter) SetMarginMode(ctx context.Context, req SetMarginModeRequest) (CommandResult, error) {
	result, err := adapter.client.SetMarginMode(ctx, sixmmccxt.SetMarginModeRequest{
		AccountConfig: toSixAccountConfig(req.AccountConfig),
		Symbol:        req.Symbol,
		MarginMode:    req.MarginMode,
		Params:        req.Params,
	})
	if err != nil {
		return CommandResult{}, fromSixError(err)
	}
	return fromSixCommandResult(result), nil
}

func toSixAccountConfig(account AccountConfig) sixmmccxt.AccountConfig {
	return sixmmccxt.AccountConfig{
		Exchange:      account.Exchange,
		AccountName:   account.AccountName,
		CCXTID:        account.CCXTID,
		MarketType:    account.MarketType,
		Sandbox:       account.Sandbox,
		DefaultSettle: account.DefaultSettle,
		AccountType:   account.AccountType,
		ProductType:   account.ProductType,
		Category:      account.Category,
		PositionMode:  account.PositionMode,
		MarginMode:    account.MarginMode,
		RecvWindowMS:  account.RecvWindowMS,
		RateLimitMS:   account.RateLimitMS,
		APIKey:        account.APIKey,
		APISecret:     account.APISecret,
		Passphrase:    account.Passphrase,
	}
}

func fromSixBalanceResult(result sixmmccxt.BalanceResult) BalanceResult {
	assets := make([]BalanceAsset, 0, len(result.Assets))
	for _, asset := range result.Assets {
		assets = append(assets, BalanceAsset{
			Asset:     asset.Asset,
			Free:      asset.Free,
			Used:      asset.Used,
			Total:     asset.Total,
			Debt:      asset.Debt,
			UpdatedAt: asset.UpdatedAt,
		})
	}
	return BalanceResult{
		Exchange:    result.Exchange,
		AccountName: result.AccountName,
		Assets:      assets,
		Raw:         result.Raw,
	}
}

func fromSixPositions(results []sixmmccxt.Position) []Position {
	positions := make([]Position, 0, len(results))
	for _, result := range results {
		positions = append(positions, Position{
			Exchange:          result.Exchange,
			AccountName:       result.AccountName,
			Symbol:            result.Symbol,
			PositionSide:      result.PositionSide,
			Quantity:          result.Quantity,
			NotionalUSDT:      result.NotionalUSDT,
			EntryPrice:        result.EntryPrice,
			MarkPrice:         result.MarkPrice,
			UnrealizedPnlUSDT: result.UnrealizedPnlUSDT,
			Leverage:          result.Leverage,
			MarginMode:        result.MarginMode,
			UpdatedAt:         result.UpdatedAt,
			Raw:               result.Raw,
		})
	}
	return positions
}

func fromSixTicker(result sixmmccxt.Ticker) Ticker {
	return Ticker{
		Exchange:  result.Exchange,
		Symbol:    result.Symbol,
		Last:      result.Last,
		Mark:      result.Mark,
		Bid:       result.Bid,
		Ask:       result.Ask,
		BidSize:   result.BidSize,
		AskSize:   result.AskSize,
		UpdatedAt: result.UpdatedAt,
		Raw:       result.Raw,
	}
}

func fromSixMarkets(results []sixmmccxt.Market) []Market {
	markets := make([]Market, 0, len(results))
	for _, result := range results {
		markets = append(markets, Market{
			Exchange:        result.Exchange,
			CCXTID:          result.CCXTID,
			Symbol:          result.Symbol,
			Base:            result.Base,
			Quote:           result.Quote,
			Settle:          result.Settle,
			MarketType:      result.MarketType,
			Active:          result.Active,
			Contract:        result.Contract,
			Linear:          result.Linear,
			Inverse:         result.Inverse,
			Spot:            result.Spot,
			Swap:            result.Swap,
			Future:          result.Future,
			Option:          result.Option,
			PricePrecision:  result.PricePrecision,
			AmountPrecision: result.AmountPrecision,
			MinAmount:       result.MinAmount,
			MinCost:         result.MinCost,
			ContractSize:    result.ContractSize,
			CreatedAt:       result.CreatedAt,
			Raw:             result.Raw,
		})
	}
	return markets
}

func fromSixOrderResult(result sixmmccxt.OrderResult) OrderResult {
	return OrderResult{
		ExchangeOrderID: result.ExchangeOrderID,
		ClientOrderID:   result.ClientOrderID,
		Status:          result.Status,
		Symbol:          result.Symbol,
		Side:            result.Side,
		OrderType:       result.OrderType,
		Quantity:        result.Quantity,
		Price:           result.Price,
		ReduceOnly:      result.ReduceOnly,
		FilledQuantity:  result.FilledQuantity,
		AvgPrice:        result.AvgPrice,
		FeeUSDT:         result.FeeUSDT,
		UpdatedAt:       result.UpdatedAt,
		Raw:             result.Raw,
	}
}

func fromSixCommandResult(result sixmmccxt.CommandResult) CommandResult {
	return CommandResult{
		Success: result.Success,
		Raw:     result.Raw,
	}
}

func fromSixError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sixmmccxt.ErrOrderNotFound):
		return ErrOrderNotFound
	case errors.Is(err, sixmmccxt.ErrOrderAlreadyFinal):
		return ErrOrderAlreadyFinal
	default:
		return err
	}
}
