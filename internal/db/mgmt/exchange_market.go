package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

const (
	ExchangeEnvPaper = "paper"
	ExchangeEnvLive  = "live"
)

type ExchangeMarketFilter struct {
	Exchange         string
	CCXTID           string
	ExchangeEnv      string
	MarketType       string
	SettleAsset      string
	Symbol           string
	NormalizedSymbol string
	Active           *bool
	Limit            int
}

type ExchangeMarketPruneFilter struct {
	Exchange                 string
	CCXTID                   string
	ExchangeEnv              string
	MarketType               string
	SettleAsset              string
	AllowedNormalizedSymbols []string
	UseAllowlist             bool
}

func (r *Repository) UpsertExchangeMarkets(ctx context.Context, markets []ExchangeMarket) error {
	if len(markets) == 0 {
		return nil
	}

	prepared, err := PrepareExchangeMarketsForSave(markets)
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "exchange"},
			{Name: "ccxt_id"},
			{Name: "exchange_env"},
			{Name: "market_type"},
			{Name: "settle_asset"},
			{Name: "normalized_symbol"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"symbol",
			"base_asset",
			"quote_asset",
			"active",
			"contract",
			"linear",
			"inverse",
			"spot",
			"swap",
			"future",
			"option",
			"price_precision",
			"amount_precision",
			"min_amount",
			"min_cost",
			"contract_size",
			"raw_response",
			"fetched_at",
			"updated_at",
		}),
	}).CreateInBatches(prepared, 500).Error
	if err != nil {
		return fmt.Errorf("upsert exchange markets: %w", err)
	}
	return nil
}

func (r *Repository) PruneExchangeMarkets(ctx context.Context, filter ExchangeMarketPruneFilter) (int64, error) {
	exchange := strings.TrimSpace(filter.Exchange)
	ccxtID := strings.ToLower(strings.TrimSpace(filter.CCXTID))
	exchangeEnv := normalizeExchangeMarketEnv(filter.ExchangeEnv)
	settleAsset := strings.ToUpper(strings.TrimSpace(filter.SettleAsset))
	marketType := strings.ToLower(strings.TrimSpace(filter.MarketType))
	if exchange == "" || ccxtID == "" || exchangeEnv == "" || settleAsset == "" {
		return 0, nil
	}
	if marketType == "" && !filter.UseAllowlist {
		return 0, nil
	}

	query := r.db.WithContext(ctx).Where(
		"exchange = ? AND ccxt_id = ? AND exchange_env = ? AND settle_asset = ?",
		exchange,
		ccxtID,
		exchangeEnv,
		settleAsset,
	)

	allowedSymbols := normalizeMarketSymbolList(filter.AllowedNormalizedSymbols)
	switch {
	case marketType != "" && filter.UseAllowlist && len(allowedSymbols) > 0:
		query = query.Where("(market_type <> ? OR normalized_symbol NOT IN ?)", marketType, allowedSymbols)
	case marketType != "" && filter.UseAllowlist:
		// 白名单为空表示没有任何交易对允许进入当前同步范围。
	case marketType != "":
		query = query.Where("market_type <> ?", marketType)
	case filter.UseAllowlist && len(allowedSymbols) > 0:
		query = query.Where("normalized_symbol NOT IN ?", allowedSymbols)
	case filter.UseAllowlist:
		// 白名单为空表示没有任何交易对允许进入当前同步范围。
	}

	result := query.Delete(&ExchangeMarket{})
	if result.Error != nil {
		return 0, fmt.Errorf("prune exchange markets: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func PrepareExchangeMarketsForSave(markets []ExchangeMarket) ([]ExchangeMarket, error) {
	prepared := make([]ExchangeMarket, 0, len(markets))
	for index := range markets {
		market := markets[index]
		if err := PrepareExchangeMarketForSave(&market); err != nil {
			return nil, err
		}
		prepared = append(prepared, market)
	}
	return dedupeExchangeMarkets(prepared), nil
}

func (r *Repository) ListExchangeMarkets(ctx context.Context, filter ExchangeMarketFilter) ([]ExchangeMarket, error) {
	query := r.db.WithContext(ctx).Model(&ExchangeMarket{})
	if value := strings.TrimSpace(filter.Exchange); value != "" {
		query = query.Where("exchange = ?", value)
	}
	if value := strings.ToLower(strings.TrimSpace(filter.CCXTID)); value != "" {
		query = query.Where("ccxt_id = ?", value)
	}
	if value := normalizeExchangeMarketEnv(filter.ExchangeEnv); value != "" {
		query = query.Where("exchange_env = ?", value)
	}
	if value := strings.ToLower(strings.TrimSpace(filter.MarketType)); value != "" {
		query = query.Where("market_type = ?", value)
	}
	if value := strings.ToUpper(strings.TrimSpace(filter.SettleAsset)); value != "" {
		query = query.Where("settle_asset = ?", value)
	}
	if value := strings.TrimSpace(filter.Symbol); value != "" {
		query = query.Where("symbol = ?", value)
	}
	if value := NormalizeMarketSymbol(filter.NormalizedSymbol); value != "" {
		query = query.Where("normalized_symbol = ?", value)
	}
	if filter.Active != nil {
		query = query.Where("active = ?", *filter.Active)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	var markets []ExchangeMarket
	if err := query.
		Order("exchange ASC, ccxt_id ASC, exchange_env ASC, market_type ASC, settle_asset ASC, normalized_symbol ASC, symbol ASC").
		Limit(limit).
		Find(&markets).Error; err != nil {
		return nil, fmt.Errorf("list exchange markets: %w", err)
	}
	return markets, nil
}

func PrepareExchangeMarketForSave(market *ExchangeMarket) error {
	now := time.Now().UTC()
	market.Exchange = strings.TrimSpace(market.Exchange)
	market.CCXTID = strings.ToLower(strings.TrimSpace(market.CCXTID))
	rawExchangeEnv := strings.TrimSpace(market.ExchangeEnv)
	market.ExchangeEnv = normalizeExchangeMarketEnv(rawExchangeEnv)
	market.MarketType = strings.ToLower(strings.TrimSpace(market.MarketType))
	market.Symbol = strings.TrimSpace(market.Symbol)
	market.BaseAsset = strings.ToUpper(strings.TrimSpace(market.BaseAsset))
	market.QuoteAsset = strings.ToUpper(strings.TrimSpace(market.QuoteAsset))
	market.SettleAsset = strings.ToUpper(strings.TrimSpace(market.SettleAsset))
	market.NormalizedSymbol = NormalizeMarketSymbol(market.NormalizedSymbol)
	if market.NormalizedSymbol == "" {
		market.NormalizedSymbol = NormalizeMarketSymbol(market.Symbol)
	}
	if market.NormalizedSymbol == "" && market.BaseAsset != "" && market.QuoteAsset != "" {
		market.NormalizedSymbol = NormalizeMarketSymbol(market.BaseAsset + market.QuoteAsset)
	}
	if market.MarketType == "" {
		market.MarketType = inferMarketType(*market)
	}
	if rawExchangeEnv == "" {
		market.ExchangeEnv = ExchangeEnvPaper
	} else if market.ExchangeEnv == "" {
		return fmt.Errorf("%w: exchange_env must be paper or live", ErrInvalidConfig)
	}
	if market.FetchedAt.IsZero() {
		market.FetchedAt = now
	}
	if market.CreatedAt.IsZero() {
		market.CreatedAt = now
	}
	market.UpdatedAt = now
	if len(market.RawResponse) == 0 {
		market.RawResponse = datatypes.JSON([]byte("{}"))
	}

	if market.Exchange == "" {
		return fmt.Errorf("%w: exchange is required", ErrInvalidConfig)
	}
	if market.Symbol == "" {
		return fmt.Errorf("%w: symbol is required", ErrInvalidConfig)
	}
	if market.NormalizedSymbol == "" {
		return fmt.Errorf("%w: normalized_symbol is required", ErrInvalidConfig)
	}
	if !contains([]string{ExchangeEnvPaper, ExchangeEnvLive}, market.ExchangeEnv) {
		return fmt.Errorf("%w: exchange_env must be paper or live", ErrInvalidConfig)
	}
	if !contains([]string{"", MarketTypeSpot, MarketTypeMargin, MarketTypeSwap, MarketTypeFuture, "option"}, market.MarketType) {
		return fmt.Errorf("%w: market_type must be spot, margin, swap, future, option, or empty", ErrInvalidConfig)
	}
	if err := validateJSONObject(market.RawResponse, "raw_response"); err != nil {
		return err
	}
	return nil
}

func RawResponseJSON(raw map[string]any) datatypes.JSON {
	if len(raw) == 0 {
		return datatypes.JSON([]byte("{}"))
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(body)
}

func dedupeExchangeMarkets(markets []ExchangeMarket) []ExchangeMarket {
	if len(markets) < 2 {
		return markets
	}

	indexes := make(map[string]int, len(markets))
	deduped := make([]ExchangeMarket, 0, len(markets))
	for _, market := range markets {
		key := exchangeMarketIdentityKey(market)
		if index, ok := indexes[key]; ok {
			deduped[index] = market
			continue
		}
		indexes[key] = len(deduped)
		deduped = append(deduped, market)
	}
	return deduped
}

func exchangeMarketIdentityKey(market ExchangeMarket) string {
	return strings.Join([]string{
		market.Exchange,
		market.CCXTID,
		market.ExchangeEnv,
		market.MarketType,
		market.SettleAsset,
		market.NormalizedSymbol,
	}, "\x00")
}

// NormalizeMarketSymbol 把交易所或 CCXT 的交易对格式归一成平台 symbol_config 使用的格式。
// 例如 BTC/USDT:USDT、BTC-USDT、BTC_USDT 和 BTCUSDT 都会归一成 BTCUSDT。
func NormalizeMarketSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ""
	}
	if index := strings.Index(symbol, ":"); index >= 0 {
		symbol = symbol[:index]
	}

	var builder strings.Builder
	builder.Grow(len(symbol))
	for _, char := range symbol {
		switch {
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func normalizeMarketSymbolList(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	normalized := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		value := NormalizeMarketSymbol(symbol)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeExchangeMarketEnv(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case ExchangeEnvPaper, ExchangeEnvLive:
		return value
	default:
		return ""
	}
}

func inferMarketType(market ExchangeMarket) string {
	switch {
	case market.Swap:
		return MarketTypeSwap
	case market.Future:
		return MarketTypeFuture
	case market.Spot:
		return MarketTypeSpot
	case market.Option:
		return "option"
	default:
		return ""
	}
}
