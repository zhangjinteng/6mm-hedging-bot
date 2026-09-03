package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type syncExchangeMarketsRequest struct {
	Account     exchangeAdapterAccountRequest `json:"account"`
	ExchangeEnv string                        `json:"exchange_env"`
	Params      map[string]any                `json:"params"`
}

type syncExchangeMarketsResponse struct {
	Synced  int                      `json:"synced"`
	Preview []exchangeMarketResponse `json:"preview"`
}

type exchangeMarketResponse struct {
	ID               uint            `json:"id"`
	Exchange         string          `json:"exchange"`
	CCXTID           string          `json:"ccxt_id"`
	ExchangeEnv      string          `json:"exchange_env"`
	MarketType       string          `json:"market_type"`
	Symbol           string          `json:"symbol"`
	NormalizedSymbol string          `json:"normalized_symbol"`
	BaseAsset        string          `json:"base_asset"`
	QuoteAsset       string          `json:"quote_asset"`
	SettleAsset      string          `json:"settle_asset"`
	Active           bool            `json:"active"`
	Contract         bool            `json:"contract"`
	Linear           bool            `json:"linear"`
	Inverse          bool            `json:"inverse"`
	Spot             bool            `json:"spot"`
	Swap             bool            `json:"swap"`
	Future           bool            `json:"future"`
	Option           bool            `json:"option"`
	PricePrecision   string          `json:"price_precision"`
	AmountPrecision  string          `json:"amount_precision"`
	MinAmount        string          `json:"min_amount"`
	MinCost          string          `json:"min_cost"`
	ContractSize     string          `json:"contract_size"`
	RawResponse      json.RawMessage `json:"raw_response"`
	FetchedAt        string          `json:"fetched_at"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

func (s *Server) syncExchangeMarkets(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}
	if !s.requireManagementRepo(c) {
		return
	}

	var req syncExchangeMarketsRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	exchangeEnv, err := resolveExchangeEnv(req.ExchangeEnv)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	account := req.Account.toAccountConfig()
	syncer := service.NewMarketSyncService(s.mgmt, adapter, exchangeEnv, s.symbolAllowlist)
	result, err := syncer.SyncAccount(c.Request.Context(), service.MarketSyncAccountInput{
		Account:     account,
		ExchangeEnv: exchangeEnv,
		Params:      req.Params,
	})
	if err != nil {
		if errors.Is(err, service.ErrMarketSyncStore) {
			respondManagementError(c, err)
			return
		}
		respondAdapterResult(c, nil, err)
		return
	}

	previewCount := len(result.Markets)
	if previewCount > 50 {
		previewCount = 50
	}
	c.JSON(http.StatusOK, syncExchangeMarketsResponse{
		Synced:  result.Synced,
		Preview: toExchangeMarketResponses(result.Markets[:previewCount]),
	})
}

func (s *Server) listExchangeMarkets(c *gin.Context) {
	if !s.requireManagementRepo(c) {
		return
	}

	filter := mgmt.ExchangeMarketFilter{
		Exchange:         c.Query("exchange"),
		CCXTID:           c.Query("ccxt_id"),
		MarketType:       c.Query("market_type"),
		SettleAsset:      c.Query("settle_asset"),
		Symbol:           c.Query("symbol"),
		NormalizedSymbol: c.Query("normalized_symbol"),
		Limit:            parseLimitQuery(c.Query("limit")),
	}
	if rawEnv := strings.TrimSpace(c.Query("exchange_env")); rawEnv != "" {
		exchangeEnv, err := resolveExchangeEnv(rawEnv)
		if err != nil {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		filter.ExchangeEnv = exchangeEnv
	}
	if rawActive := strings.TrimSpace(c.Query("active")); rawActive != "" {
		active, err := strconv.ParseBool(rawActive)
		if err != nil {
			respondError(c, http.StatusBadRequest, errors.New("active must be true or false"))
			return
		}
		filter.Active = &active
	}

	markets, err := s.mgmt.ListExchangeMarkets(c.Request.Context(), filter)
	if err != nil {
		respondManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toExchangeMarketResponses(markets)})
}

func toExchangeMarketResponses(markets []mgmt.ExchangeMarket) []exchangeMarketResponse {
	responses := make([]exchangeMarketResponse, 0, len(markets))
	for _, market := range markets {
		responses = append(responses, exchangeMarketResponse{
			ID:               market.ID,
			Exchange:         market.Exchange,
			CCXTID:           market.CCXTID,
			ExchangeEnv:      market.ExchangeEnv,
			MarketType:       market.MarketType,
			Symbol:           market.Symbol,
			NormalizedSymbol: market.NormalizedSymbol,
			BaseAsset:        market.BaseAsset,
			QuoteAsset:       market.QuoteAsset,
			SettleAsset:      market.SettleAsset,
			Active:           market.Active,
			Contract:         market.Contract,
			Linear:           market.Linear,
			Inverse:          market.Inverse,
			Spot:             market.Spot,
			Swap:             market.Swap,
			Future:           market.Future,
			Option:           market.Option,
			PricePrecision:   market.PricePrecision.String(),
			AmountPrecision:  market.AmountPrecision.String(),
			MinAmount:        market.MinAmount.String(),
			MinCost:          market.MinCost.String(),
			ContractSize:     market.ContractSize.String(),
			RawResponse:      json.RawMessage(market.RawResponse),
			FetchedAt:        market.FetchedAt.Format(time.RFC3339Nano),
			CreatedAt:        market.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt:        market.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	return responses
}

func resolveExchangeEnv(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(os.Getenv("EXCHANGE_ENV")))
	}
	if value == "" {
		return mgmt.ExchangeEnvPaper, nil
	}
	switch value {
	case mgmt.ExchangeEnvPaper, mgmt.ExchangeEnvLive:
		return value, nil
	default:
		return "", errors.New("exchange_env must be paper or live")
	}
}

func parseLimitQuery(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func (s *Server) requireManagementRepo(c *gin.Context) bool {
	if s.mgmt == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("management repository is not configured"))
		return false
	}
	return true
}
