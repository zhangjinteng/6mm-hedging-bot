package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

var ErrMarketSyncStore = errors.New("market sync store error")

type MarketSyncRepository interface {
	ListActiveExchangeAccounts(ctx context.Context) ([]mgmt.ExchangeAccount, error)
	UpsertExchangeMarkets(ctx context.Context, markets []mgmt.ExchangeMarket) error
	PruneExchangeMarkets(ctx context.Context, filter mgmt.ExchangeMarketPruneFilter) (int64, error)
}

type SymbolAllowlistProvider interface {
	ListEnabledNormalizedSymbols(ctx context.Context) (map[string]struct{}, error)
}

type MarketSyncService struct {
	repo            MarketSyncRepository
	exchange        exchange.Adapter
	exchangeEnv     string
	symbolAllowlist SymbolAllowlistProvider
}

type MarketSyncAccountInput struct {
	Account     exchange.AccountConfig
	ExchangeEnv string
	Params      map[string]any
}

type MarketSyncResult struct {
	Synced  int
	Markets []mgmt.ExchangeMarket
}

type MarketSyncFailure struct {
	AccountID   uint
	Exchange    string
	AccountName string
	CCXTID      string
	Error       string
}

type MarketSyncBatchResult struct {
	Accounts      int
	SyncedMarkets int
	Failures      []MarketSyncFailure
}

func NewMarketSyncService(repo MarketSyncRepository, adapter exchange.Adapter, exchangeEnv string, allowlistProviders ...SymbolAllowlistProvider) *MarketSyncService {
	var symbolAllowlist SymbolAllowlistProvider
	if len(allowlistProviders) > 0 {
		symbolAllowlist = allowlistProviders[0]
	}
	return &MarketSyncService{
		repo:            repo,
		exchange:        adapter,
		exchangeEnv:     normalizeMarketSyncEnv(exchangeEnv),
		symbolAllowlist: symbolAllowlist,
	}
}

func (s *MarketSyncService) SyncAccount(ctx context.Context, input MarketSyncAccountInput) (MarketSyncResult, error) {
	if s == nil || s.repo == nil {
		return MarketSyncResult{}, fmt.Errorf("market sync repository is not configured")
	}
	if s.exchange == nil {
		return MarketSyncResult{}, fmt.Errorf("exchange adapter is not configured")
	}

	allowlist, allowlistConfigured, err := s.loadSymbolAllowlist(ctx)
	if err != nil {
		return MarketSyncResult{}, err
	}
	return s.syncAccount(ctx, input, allowlist, allowlistConfigured)
}

func (s *MarketSyncService) syncAccount(
	ctx context.Context,
	input MarketSyncAccountInput,
	allowlist map[string]struct{},
	allowlistConfigured bool,
) (MarketSyncResult, error) {
	exchangeEnv := normalizeMarketSyncEnv(input.ExchangeEnv)
	if exchangeEnv == "" {
		exchangeEnv = s.exchangeEnv
	}
	if exchangeEnv == "" {
		exchangeEnv = mgmt.ExchangeEnvPaper
	}

	markets, err := s.exchange.FetchMarkets(ctx, exchange.FetchMarketsRequest{
		AccountConfig: input.Account,
		Params:        input.Params,
	})
	if err != nil {
		return MarketSyncResult{}, err
	}

	models := ExchangeMarketModelsFromMarkets(markets, input.Account, exchangeEnv)
	prepared, err := mgmt.PrepareExchangeMarketsForSave(models)
	if err != nil {
		return MarketSyncResult{}, fmt.Errorf("%w: %v", ErrMarketSyncStore, err)
	}
	prepared = filterMarketsByAccountMarketType(prepared, input.Account.MarketType)
	prepared = filterMarketsByAllowlist(prepared, allowlist, allowlistConfigured)
	if err := s.repo.UpsertExchangeMarkets(ctx, prepared); err != nil {
		return MarketSyncResult{}, fmt.Errorf("%w: %v", ErrMarketSyncStore, err)
	}
	if _, err := s.repo.PruneExchangeMarkets(ctx, buildExchangeMarketPruneFilter(input.Account, exchangeEnv, allowlist, allowlistConfigured)); err != nil {
		return MarketSyncResult{}, fmt.Errorf("%w: %v", ErrMarketSyncStore, err)
	}
	return MarketSyncResult{Synced: len(prepared), Markets: prepared}, nil
}

func (s *MarketSyncService) SyncActiveAccounts(ctx context.Context) (MarketSyncBatchResult, error) {
	if s == nil || s.repo == nil {
		return MarketSyncBatchResult{}, fmt.Errorf("market sync repository is not configured")
	}
	accounts, err := s.repo.ListActiveExchangeAccounts(ctx)
	if err != nil {
		return MarketSyncBatchResult{}, err
	}

	allowlist, allowlistConfigured, err := s.loadSymbolAllowlist(ctx)
	if err != nil {
		return MarketSyncBatchResult{}, err
	}

	seen := make(map[string]struct{}, len(accounts))
	var result MarketSyncBatchResult
	for _, account := range accounts {
		key := marketSyncAccountKey(account)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result.Accounts++

		syncResult, err := s.syncAccount(ctx, MarketSyncAccountInput{
			Account: ExchangeAccountConfigFromModel(account),
		}, allowlist, allowlistConfigured)
		if err != nil {
			result.Failures = append(result.Failures, MarketSyncFailure{
				AccountID:   account.ID,
				Exchange:    account.Exchange,
				AccountName: account.Name,
				CCXTID:      account.CCXTID,
				Error:       err.Error(),
			})
			continue
		}
		result.SyncedMarkets += syncResult.Synced
	}
	return result, nil
}

func ExchangeAccountConfigFromModel(account mgmt.ExchangeAccount) exchange.AccountConfig {
	return exchange.AccountConfig{
		Exchange:      account.Exchange,
		AccountName:   account.Name,
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
	}
}

func ExchangeMarketModelsFromMarkets(markets []exchange.Market, account exchange.AccountConfig, exchangeEnv string) []mgmt.ExchangeMarket {
	models := make([]mgmt.ExchangeMarket, 0, len(markets))
	fetchedAt := time.Now().UTC()
	for _, market := range markets {
		exchangeName := strings.TrimSpace(market.Exchange)
		if exchangeName == "" {
			exchangeName = account.Exchange
		}
		ccxtID := strings.TrimSpace(market.CCXTID)
		if ccxtID == "" {
			ccxtID = account.CCXTID
		}
		marketType := strings.TrimSpace(market.MarketType)
		if marketType == "" {
			marketType = account.MarketType
		}
		settle := strings.TrimSpace(market.Settle)
		if settle == "" {
			settle = account.DefaultSettle
		}
		models = append(models, mgmt.ExchangeMarket{
			Exchange:         exchangeName,
			CCXTID:           ccxtID,
			ExchangeEnv:      exchangeEnv,
			MarketType:       marketType,
			Symbol:           market.Symbol,
			NormalizedSymbol: mgmt.NormalizeMarketSymbol(market.Symbol),
			BaseAsset:        market.Base,
			QuoteAsset:       market.Quote,
			SettleAsset:      settle,
			Active:           market.Active,
			Contract:         market.Contract,
			Linear:           market.Linear,
			Inverse:          market.Inverse,
			Spot:             market.Spot,
			Swap:             market.Swap,
			Future:           market.Future,
			Option:           market.Option,
			PricePrecision:   market.PricePrecision,
			AmountPrecision:  market.AmountPrecision,
			MinAmount:        market.MinAmount,
			MinCost:          market.MinCost,
			ContractSize:     market.ContractSize,
			RawResponse:      mgmt.RawResponseJSON(market.Raw),
			FetchedAt:        fetchedAt,
		})
	}
	return models
}

func filterMarketsByAccountMarketType(markets []mgmt.ExchangeMarket, marketType string) []mgmt.ExchangeMarket {
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	if marketType == "" || len(markets) == 0 {
		return markets
	}

	filtered := make([]mgmt.ExchangeMarket, 0, len(markets))
	for _, market := range markets {
		if strings.EqualFold(strings.TrimSpace(market.MarketType), marketType) {
			filtered = append(filtered, market)
		}
	}
	return filtered
}

func (s *MarketSyncService) loadSymbolAllowlist(ctx context.Context) (map[string]struct{}, bool, error) {
	if s.symbolAllowlist == nil {
		return nil, false, nil
	}
	allowlist, err := s.symbolAllowlist.ListEnabledNormalizedSymbols(ctx)
	if err != nil {
		return nil, true, fmt.Errorf("%w: list symbol config allowlist: %v", ErrMarketSyncStore, err)
	}
	return allowlist, true, nil
}

func filterMarketsByAllowlist(markets []mgmt.ExchangeMarket, allowlist map[string]struct{}, configured bool) []mgmt.ExchangeMarket {
	if !configured {
		return markets
	}
	if len(allowlist) == 0 || len(markets) == 0 {
		return nil
	}

	filtered := make([]mgmt.ExchangeMarket, 0, len(markets))
	for _, market := range markets {
		normalizedSymbol := mgmt.NormalizeMarketSymbol(market.NormalizedSymbol)
		if normalizedSymbol == "" {
			normalizedSymbol = mgmt.NormalizeMarketSymbol(market.Symbol)
		}
		if _, ok := allowlist[normalizedSymbol]; ok {
			market.NormalizedSymbol = normalizedSymbol
			filtered = append(filtered, market)
		}
	}
	return filtered
}

func buildExchangeMarketPruneFilter(
	account exchange.AccountConfig,
	exchangeEnv string,
	allowlist map[string]struct{},
	allowlistConfigured bool,
) mgmt.ExchangeMarketPruneFilter {
	return mgmt.ExchangeMarketPruneFilter{
		Exchange:                 account.Exchange,
		CCXTID:                   account.CCXTID,
		ExchangeEnv:              exchangeEnv,
		MarketType:               account.MarketType,
		SettleAsset:              account.DefaultSettle,
		AllowedNormalizedSymbols: normalizedSymbolsFromAllowlist(allowlist),
		UseAllowlist:             allowlistConfigured,
	}
}

func normalizedSymbolsFromAllowlist(allowlist map[string]struct{}) []string {
	symbols := make([]string, 0, len(allowlist))
	for symbol := range allowlist {
		if normalized := mgmt.NormalizeMarketSymbol(symbol); normalized != "" {
			symbols = append(symbols, normalized)
		}
	}
	return symbols
}

func normalizeMarketSyncEnv(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case mgmt.ExchangeEnvPaper:
		return mgmt.ExchangeEnvPaper
	case mgmt.ExchangeEnvLive:
		return mgmt.ExchangeEnvLive
	default:
		return ""
	}
}

func marketSyncAccountKey(account mgmt.ExchangeAccount) string {
	parts := []string{
		account.Exchange,
		account.CCXTID,
		account.MarketType,
		account.DefaultSettle,
		account.AccountType,
		account.ProductType,
		account.Category,
	}
	return strings.ToLower(strings.Join(parts, "|"))
}
