package service

import (
	"context"
	"testing"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

type fakeMarketSyncRepository struct {
	accounts []mgmt.ExchangeAccount
	markets  []mgmt.ExchangeMarket
}

type fakeSymbolAllowlistProvider struct {
	symbols map[string]struct{}
	err     error
}

type fakeMarketAdapter struct {
	exchange.Adapter
	markets []exchange.Market
	err     error
}

func (repo *fakeMarketSyncRepository) ListActiveExchangeAccounts(ctx context.Context) ([]mgmt.ExchangeAccount, error) {
	return repo.accounts, ctx.Err()
}

func (repo *fakeMarketSyncRepository) UpsertExchangeMarkets(ctx context.Context, markets []mgmt.ExchangeMarket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repo.markets = append(repo.markets, markets...)
	return nil
}

func (repo *fakeMarketSyncRepository) PruneExchangeMarkets(ctx context.Context, filter mgmt.ExchangeMarketPruneFilter) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	allowed := make(map[string]struct{}, len(filter.AllowedNormalizedSymbols))
	for _, symbol := range filter.AllowedNormalizedSymbols {
		if normalized := mgmt.NormalizeMarketSymbol(symbol); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}

	var deleted int64
	retained := make([]mgmt.ExchangeMarket, 0, len(repo.markets))
	for _, market := range repo.markets {
		inScope := market.Exchange == filter.Exchange &&
			market.CCXTID == filter.CCXTID &&
			market.ExchangeEnv == filter.ExchangeEnv &&
			market.SettleAsset == filter.SettleAsset
		if !inScope {
			retained = append(retained, market)
			continue
		}

		marketTypeAllowed := filter.MarketType == "" || market.MarketType == filter.MarketType
		symbolAllowed := !filter.UseAllowlist
		if filter.UseAllowlist {
			_, symbolAllowed = allowed[mgmt.NormalizeMarketSymbol(market.NormalizedSymbol)]
		}
		if marketTypeAllowed && symbolAllowed {
			retained = append(retained, market)
			continue
		}
		deleted++
	}
	repo.markets = retained
	return deleted, nil
}

func (provider *fakeSymbolAllowlistProvider) ListEnabledNormalizedSymbols(ctx context.Context) (map[string]struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return provider.symbols, provider.err
}

func (adapter fakeMarketAdapter) FetchMarkets(ctx context.Context, req exchange.FetchMarketsRequest) ([]exchange.Market, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return adapter.markets, adapter.err
}

func TestMarketSyncServiceSyncActiveAccountsDeduplicatesMarketSources(t *testing.T) {
	repo := &fakeMarketSyncRepository{
		accounts: []mgmt.ExchangeAccount{
			{
				ID:            1,
				Exchange:      "Binance",
				Name:          "main",
				CCXTID:        "binanceusdm",
				MarketType:    "swap",
				Sandbox:       true,
				DefaultSettle: "USDT",
				Category:      "linear",
			},
			{
				ID:            2,
				Exchange:      "Binance",
				Name:          "backup",
				CCXTID:        "binanceusdm",
				MarketType:    "swap",
				Sandbox:       true,
				DefaultSettle: "USDT",
				Category:      "linear",
			},
		},
	}
	syncer := NewMarketSyncService(repo, exchange.NewSimulatedAdapter(), mgmt.ExchangeEnvLive)

	result, err := syncer.SyncActiveAccounts(context.Background())
	if err != nil {
		t.Fatalf("sync active accounts: %v", err)
	}
	if result.Accounts != 1 {
		t.Fatalf("expected one deduplicated account source, got %d", result.Accounts)
	}
	if result.SyncedMarkets != 3 || len(repo.markets) != 3 {
		t.Fatalf("unexpected market count result=%+v saved=%d", result, len(repo.markets))
	}
	for _, market := range repo.markets {
		if market.ExchangeEnv != mgmt.ExchangeEnvLive {
			t.Fatalf("expected live exchange env, got %s", market.ExchangeEnv)
		}
		if market.Exchange != "Binance" || market.CCXTID != "binanceusdm" || market.MarketType != "swap" {
			t.Fatalf("unexpected market identity %+v", market)
		}
	}
}

func TestMarketSyncServiceFiltersMarketsBySymbolConfigAllowlist(t *testing.T) {
	repo := &fakeMarketSyncRepository{
		accounts: []mgmt.ExchangeAccount{
			{
				ID:            1,
				Exchange:      "Binance",
				Name:          "main",
				CCXTID:        "binanceusdm",
				MarketType:    "swap",
				Sandbox:       true,
				DefaultSettle: "USDT",
				Category:      "linear",
			},
		},
	}
	allowlist := &fakeSymbolAllowlistProvider{
		symbols: map[string]struct{}{
			"BTCUSDT": {},
		},
	}
	syncer := NewMarketSyncService(repo, exchange.NewSimulatedAdapter(), mgmt.ExchangeEnvPaper, allowlist)

	result, err := syncer.SyncActiveAccounts(context.Background())
	if err != nil {
		t.Fatalf("sync active accounts: %v", err)
	}
	if result.SyncedMarkets != 1 || len(repo.markets) != 1 {
		t.Fatalf("expected one allowed market, result=%+v saved=%d", result, len(repo.markets))
	}
	if repo.markets[0].Symbol != "BTC/USDT:USDT" || repo.markets[0].NormalizedSymbol != "BTCUSDT" {
		t.Fatalf("unexpected saved market %+v", repo.markets[0])
	}
}

func TestMarketSyncServiceFiltersMarketsByAccountMarketType(t *testing.T) {
	repo := &fakeMarketSyncRepository{
		accounts: []mgmt.ExchangeAccount{
			{
				ID:            1,
				Exchange:      "Bybit",
				Name:          "market sync",
				CCXTID:        "bybit",
				MarketType:    "swap",
				Sandbox:       true,
				DefaultSettle: "USDT",
				Category:      "linear",
			},
		},
	}
	adapter := fakeMarketAdapter{
		markets: []exchange.Market{
			{Exchange: "Bybit", CCXTID: "bybit", Symbol: "BTC/USDT", MarketType: "spot", Base: "BTC", Quote: "USDT", Settle: "USDT", Spot: true, Active: true},
			{Exchange: "Bybit", CCXTID: "bybit", Symbol: "BTC/USDT:USDT", MarketType: "swap", Base: "BTC", Quote: "USDT", Settle: "USDT", Swap: true, Contract: true, Active: true},
			{Exchange: "Bybit", CCXTID: "bybit", Symbol: "BTC/USDT:USDT-261030", MarketType: "future", Base: "BTC", Quote: "USDT", Settle: "USDT", Future: true, Contract: true, Active: true},
			{Exchange: "Bybit", CCXTID: "bybit", Symbol: "BTC/USDT:USDT-260904-77000-C", MarketType: "option", Base: "BTC", Quote: "USDT", Settle: "USDT", Option: true, Contract: true, Active: true},
		},
	}
	allowlist := &fakeSymbolAllowlistProvider{
		symbols: map[string]struct{}{
			"BTCUSDT": {},
		},
	}
	syncer := NewMarketSyncService(repo, adapter, mgmt.ExchangeEnvPaper, allowlist)

	result, err := syncer.SyncActiveAccounts(context.Background())
	if err != nil {
		t.Fatalf("sync active accounts: %v", err)
	}
	if result.SyncedMarkets != 1 || len(repo.markets) != 1 {
		t.Fatalf("expected one swap market, result=%+v saved=%d", result, len(repo.markets))
	}
	if repo.markets[0].MarketType != "swap" || repo.markets[0].Symbol != "BTC/USDT:USDT" {
		t.Fatalf("unexpected saved market %+v", repo.markets[0])
	}
}

func TestMarketSyncServicePrunesHistoricalMarketsOutsideAccountMarketType(t *testing.T) {
	repo := &fakeMarketSyncRepository{
		accounts: []mgmt.ExchangeAccount{
			{
				ID:            1,
				Exchange:      "Bybit",
				Name:          "market sync",
				CCXTID:        "bybit",
				MarketType:    "swap",
				Sandbox:       true,
				DefaultSettle: "USDT",
				Category:      "linear",
			},
		},
		markets: []mgmt.ExchangeMarket{
			{Exchange: "Bybit", CCXTID: "bybit", ExchangeEnv: mgmt.ExchangeEnvPaper, MarketType: "spot", SettleAsset: "USDT", Symbol: "BTC/USDT", NormalizedSymbol: "BTCUSDT"},
			{Exchange: "Bybit", CCXTID: "bybit", ExchangeEnv: mgmt.ExchangeEnvPaper, MarketType: "future", SettleAsset: "USDT", Symbol: "BTC/USDT:USDT-261030", NormalizedSymbol: "BTCUSDT"},
			{Exchange: "OKX", CCXTID: "okx", ExchangeEnv: mgmt.ExchangeEnvPaper, MarketType: "spot", SettleAsset: "USDT", Symbol: "BTC/USDT", NormalizedSymbol: "BTCUSDT"},
		},
	}
	adapter := fakeMarketAdapter{
		markets: []exchange.Market{
			{Exchange: "Bybit", CCXTID: "bybit", Symbol: "BTC/USDT:USDT", MarketType: "swap", Base: "BTC", Quote: "USDT", Settle: "USDT", Swap: true, Contract: true, Active: true},
		},
	}
	allowlist := &fakeSymbolAllowlistProvider{
		symbols: map[string]struct{}{
			"BTCUSDT": {},
		},
	}
	syncer := NewMarketSyncService(repo, adapter, mgmt.ExchangeEnvPaper, allowlist)

	result, err := syncer.SyncActiveAccounts(context.Background())
	if err != nil {
		t.Fatalf("sync active accounts: %v", err)
	}
	if result.SyncedMarkets != 1 {
		t.Fatalf("expected one synced market, got %+v", result)
	}
	if len(repo.markets) != 2 {
		t.Fatalf("expected Bybit historical spot/future pruned and OKX retained, got %d", len(repo.markets))
	}
	for _, market := range repo.markets {
		if market.Exchange == "Bybit" && market.MarketType != "swap" {
			t.Fatalf("unexpected non-swap Bybit market retained: %+v", market)
		}
	}
}
