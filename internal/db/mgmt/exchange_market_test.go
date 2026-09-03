package mgmt

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestPrepareExchangeMarketForSaveNormalizesDefaults(t *testing.T) {
	market := ExchangeMarket{
		Exchange:       " Binance ",
		CCXTID:         "BINANCEUSDM",
		MarketType:     "",
		Symbol:         " BTC/USDT:USDT ",
		BaseAsset:      "btc",
		QuoteAsset:     "usdt",
		SettleAsset:    "usdt",
		Swap:           true,
		Active:         true,
		PricePrecision: decimal.RequireFromString("0.1"),
		RawResponse:    []byte(`{"id":"BTCUSDT"}`),
	}

	if err := PrepareExchangeMarketForSave(&market); err != nil {
		t.Fatalf("prepare market: %v", err)
	}
	if market.Exchange != "Binance" || market.CCXTID != "binanceusdm" {
		t.Fatalf("unexpected exchange identity %+v", market)
	}
	if market.ExchangeEnv != ExchangeEnvPaper {
		t.Fatalf("expected default paper env, got %s", market.ExchangeEnv)
	}
	if market.MarketType != MarketTypeSwap {
		t.Fatalf("expected inferred swap market type, got %s", market.MarketType)
	}
	if market.Symbol != "BTC/USDT:USDT" || market.BaseAsset != "BTC" || market.QuoteAsset != "USDT" || market.SettleAsset != "USDT" {
		t.Fatalf("unexpected market symbols %+v", market)
	}
	if market.NormalizedSymbol != "BTCUSDT" {
		t.Fatalf("unexpected normalized symbol %s", market.NormalizedSymbol)
	}
	if market.FetchedAt.IsZero() || market.CreatedAt.IsZero() || market.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be filled: %+v", market)
	}
}

func TestNormalizeMarketSymbol(t *testing.T) {
	tests := map[string]string{
		"BTC/USDT:USDT": "BTCUSDT",
		"btc-usdt":      "BTCUSDT",
		"BTC_USDT":      "BTCUSDT",
		" BTCUSDT ":     "BTCUSDT",
		"ETH/USDC:USDC": "ETHUSDC",
	}

	for input, expected := range tests {
		if actual := NormalizeMarketSymbol(input); actual != expected {
			t.Fatalf("NormalizeMarketSymbol(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestPrepareExchangeMarketForSaveRejectsInvalidEnv(t *testing.T) {
	market := ExchangeMarket{
		Exchange:    "Binance",
		CCXTID:      "binanceusdm",
		ExchangeEnv: "demo",
		MarketType:  MarketTypeSwap,
		Symbol:      "BTC/USDT:USDT",
		RawResponse: []byte(`{}`),
	}

	err := PrepareExchangeMarketForSave(&market)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
}

func TestDedupeExchangeMarketsKeepsLastForSameIdentity(t *testing.T) {
	markets := []ExchangeMarket{
		{
			Exchange:     "Binance",
			CCXTID:       "binanceusdm",
			ExchangeEnv:  ExchangeEnvPaper,
			MarketType:   MarketTypeSwap,
			SettleAsset:  "USDT",
			Symbol:       "BTCUSDT",
			Active:       false,
			RawResponse:  []byte(`{"version":"old"}`),
			MinCost:      decimal.RequireFromString("5"),
			ContractSize: decimal.NewFromInt(1),
		},
		{
			Exchange:     "Binance",
			CCXTID:       "binanceusdm",
			ExchangeEnv:  ExchangeEnvPaper,
			MarketType:   MarketTypeSwap,
			SettleAsset:  "USDT",
			Symbol:       "BTC/USDT:USDT",
			Active:       true,
			RawResponse:  []byte(`{"version":"new"}`),
			MinCost:      decimal.RequireFromString("10"),
			ContractSize: decimal.NewFromInt(1),
		},
		{
			Exchange:     "Binance",
			CCXTID:       "binanceusdm",
			ExchangeEnv:  ExchangeEnvPaper,
			MarketType:   MarketTypeSwap,
			SettleAsset:  "USDT",
			Symbol:       "ETH/USDT:USDT",
			Active:       true,
			RawResponse:  []byte(`{"version":"eth"}`),
			ContractSize: decimal.NewFromInt(1),
		},
	}

	prepared, err := PrepareExchangeMarketsForSave(markets)
	if err != nil {
		t.Fatalf("prepare markets: %v", err)
	}
	if len(prepared) != 2 {
		t.Fatalf("expected two unique markets, got %d", len(prepared))
	}
	if prepared[0].Symbol != "BTC/USDT:USDT" || prepared[0].NormalizedSymbol != "BTCUSDT" || !prepared[0].Active || !prepared[0].MinCost.Equal(decimal.RequireFromString("10")) {
		t.Fatalf("expected last BTC market to win, got %+v", prepared[0])
	}
	if string(prepared[0].RawResponse) != `{"version":"new"}` {
		t.Fatalf("expected last raw response, got %s", prepared[0].RawResponse)
	}
	if prepared[1].Symbol != "ETH/USDT:USDT" || prepared[1].NormalizedSymbol != "ETHUSDT" {
		t.Fatalf("expected ETH market to keep order, got %+v", prepared[1])
	}
}
