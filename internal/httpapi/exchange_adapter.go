package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type exchangeAdapterAccountRequest struct {
	ID            uint   `json:"id"`
	Exchange      string `json:"exchange"`
	AccountName   string `json:"account_name"`
	CCXTID        string `json:"ccxt_id"`
	MarketType    string `json:"market_type"`
	Sandbox       bool   `json:"sandbox"`
	DefaultSettle string `json:"default_settle"`
	AccountType   string `json:"account_type"`
	ProductType   string `json:"product_type"`
	Category      string `json:"category"`
	PositionMode  string `json:"position_mode"`
	MarginMode    string `json:"margin_mode"`
	RecvWindowMS  int    `json:"recv_window_ms"`
	RateLimitMS   int    `json:"rate_limit_ms"`
	APIKey        string `json:"api_key"`
	APISecret     string `json:"api_secret"`
	Passphrase    string `json:"passphrase"`
}

type fetchAdapterBalanceRequest struct {
	Account exchangeAdapterAccountRequest `json:"account"`
	Assets  []string                      `json:"assets"`
	Params  map[string]any                `json:"params"`
}

type fetchAdapterPositionsRequest struct {
	Account exchangeAdapterAccountRequest `json:"account"`
	Symbols []string                      `json:"symbols"`
	Params  map[string]any                `json:"params"`
}

type closeAdapterPositionsRequest struct {
	Account             exchangeAdapterAccountRequest `json:"account"`
	Symbols             []string                      `json:"symbols"`
	Params              map[string]any                `json:"params"`
	ExchangeOrderParams map[string]any                `json:"exchange_order_params"`
	ConfirmClose        bool                          `json:"confirm_close"`
}

type closeAdapterPositionResult struct {
	Symbol           string                `json:"symbol"`
	PositionSide     string                `json:"position_side"`
	PositionQuantity decimal.Decimal       `json:"position_quantity"`
	CloseSide        string                `json:"close_side"`
	CloseQuantity    decimal.Decimal       `json:"close_quantity"`
	Order            *exchange.OrderResult `json:"order,omitempty"`
	Error            string                `json:"error,omitempty"`
}

type closeAdapterPositionsResponse struct {
	Success          bool                         `json:"success"`
	FetchedPositions int                          `json:"fetched_positions"`
	AttemptedOrders  int                          `json:"attempted_orders"`
	SuccessfulOrders int                          `json:"successful_orders"`
	FailedOrders     int                          `json:"failed_orders"`
	Items            []closeAdapterPositionResult `json:"items"`
}

type fetchAdapterTickerRequest struct {
	Account exchangeAdapterAccountRequest `json:"account"`
	Symbol  string                        `json:"symbol"`
	Params  map[string]any                `json:"params"`
}

type fetchAdapterMarketsRequest struct {
	Account exchangeAdapterAccountRequest `json:"account"`
	Params  map[string]any                `json:"params"`
}

type placeAdapterOrderRequest struct {
	Account             exchangeAdapterAccountRequest `json:"account"`
	ClientOrderID       string                        `json:"client_order_id"`
	Symbol              string                        `json:"symbol"`
	Side                string                        `json:"side"`
	PositionSide        string                        `json:"position_side"`
	OrderType           string                        `json:"order_type"`
	Quantity            adapterDecimal                `json:"quantity"`
	Price               adapterDecimal                `json:"price"`
	ReduceOnly          bool                          `json:"reduce_only"`
	ExchangeOrderParams map[string]any                `json:"exchange_order_params"`
}

type adapterDecimal struct {
	decimal.Decimal
}

type fetchAdapterOrderRequest struct {
	Account       exchangeAdapterAccountRequest `json:"account"`
	OrderID       string                        `json:"order_id"`
	ClientOrderID string                        `json:"client_order_id"`
	Symbol        string                        `json:"symbol"`
	Params        map[string]any                `json:"params"`
}

type cancelAdapterOrderRequest struct {
	Account       exchangeAdapterAccountRequest `json:"account"`
	OrderID       string                        `json:"order_id"`
	ClientOrderID string                        `json:"client_order_id"`
	Symbol        string                        `json:"symbol"`
	Params        map[string]any                `json:"params"`
}

type setAdapterLeverageRequest struct {
	Account  exchangeAdapterAccountRequest `json:"account"`
	Symbol   string                        `json:"symbol"`
	Leverage int                           `json:"leverage"`
	Params   map[string]any                `json:"params"`
}

type setAdapterMarginModeRequest struct {
	Account    exchangeAdapterAccountRequest `json:"account"`
	Symbol     string                        `json:"symbol"`
	MarginMode string                        `json:"margin_mode"`
	Params     map[string]any                `json:"params"`
}

func (s *Server) fetchAdapterBalance(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req fetchAdapterBalanceRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.FetchBalance(c.Request.Context(), exchange.FetchBalanceRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Assets:        cleanStringSlice(req.Assets),
		Params:        req.Params,
	})
	respondAdapterResult(c, result, err)
}

func (s *Server) fetchAdapterPositions(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req fetchAdapterPositionsRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.FetchPositions(c.Request.Context(), exchange.FetchPositionsRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Symbols:       cleanStringSlice(req.Symbols),
		Params:        req.Params,
	})
	respondAdapterResult(c, gin.H{"items": result}, err)
}

func (s *Server) closeAdapterPositions(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req closeAdapterPositionsRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	if !req.ConfirmClose {
		respondError(c, http.StatusBadRequest, errors.New("confirm_close must be true"))
		return
	}

	positions, err := adapter.FetchPositions(c.Request.Context(), exchange.FetchPositionsRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Symbols:       cleanStringSlice(req.Symbols),
		Params:        req.Params,
	})
	if err != nil {
		respondAdapterResult(c, nil, err)
		return
	}

	response := closeAdapterPositionsResponse{
		Success:          true,
		FetchedPositions: len(positions),
		Items:            make([]closeAdapterPositionResult, 0, len(positions)),
	}
	requestID := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	for index, position := range positions {
		if position.Quantity.IsZero() {
			continue
		}

		orderReq := buildCloseAdapterOrderRequest(
			req.Account,
			position,
			"close_"+requestID+"_"+strconv.Itoa(index+1),
			req.ExchangeOrderParams,
		)
		item := closeAdapterPositionResult{
			Symbol:           position.Symbol,
			PositionSide:     orderReq.PositionSide,
			PositionQuantity: position.Quantity,
			CloseSide:        orderReq.Side,
			CloseQuantity:    orderReq.Quantity,
		}
		response.AttemptedOrders++

		order, placeErr := adapter.PlaceOrder(c.Request.Context(), orderReq)
		if placeErr != nil {
			item.Error = placeErr.Error()
			response.FailedOrders++
			response.Success = false
		} else {
			item.Order = &order
			response.SuccessfulOrders++
			s.scheduleOrderReconciliation(req.Account, order, position.Symbol, "", orderReq.ClientOrderID)
		}
		response.Items = append(response.Items, item)
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) fetchAdapterTicker(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req fetchAdapterTickerRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.FetchTicker(c.Request.Context(), exchange.FetchTickerRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Symbol:        strings.TrimSpace(req.Symbol),
		Params:        req.Params,
	})
	respondAdapterResult(c, result, err)
}

func (s *Server) fetchAdapterMarkets(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req fetchAdapterMarketsRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.FetchMarkets(c.Request.Context(), exchange.FetchMarketsRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Params:        req.Params,
	})
	respondAdapterResult(c, gin.H{"items": result}, err)
}

func (s *Server) placeAdapterOrder(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req placeAdapterOrderRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.PlaceOrder(c.Request.Context(), req.toPlaceOrderRequest())
	if err == nil {
		s.scheduleOrderReconciliation(req.Account, result, req.Symbol, "", req.ClientOrderID)
	}
	respondAdapterResult(c, result, err)
}

func (s *Server) fetchAdapterOrder(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req fetchAdapterOrderRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.FetchOrder(c.Request.Context(), exchange.FetchOrderRequest{
		AccountConfig: req.Account.toAccountConfig(),
		OrderID:       strings.TrimSpace(req.OrderID),
		ClientOrderID: strings.TrimSpace(req.ClientOrderID),
		Symbol:        strings.TrimSpace(req.Symbol),
		Params:        req.Params,
	})
	respondAdapterResult(c, result, err)
}

func (s *Server) cancelAdapterOrder(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req cancelAdapterOrderRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.CancelOrder(c.Request.Context(), exchange.CancelOrderRequest{
		AccountConfig: req.Account.toAccountConfig(),
		OrderID:       strings.TrimSpace(req.OrderID),
		ClientOrderID: strings.TrimSpace(req.ClientOrderID),
		Symbol:        strings.TrimSpace(req.Symbol),
		Params:        req.Params,
	})
	if err == nil {
		s.scheduleOrderReconciliation(req.Account, result, req.Symbol, req.OrderID, req.ClientOrderID)
	}
	respondAdapterResult(c, result, err)
}

func (s *Server) setAdapterLeverage(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req setAdapterLeverageRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.SetLeverage(c.Request.Context(), exchange.SetLeverageRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Symbol:        strings.TrimSpace(req.Symbol),
		Leverage:      req.Leverage,
		Params:        req.Params,
	})
	if err == nil && result.Success {
		s.schedulePositionSettingVerification(req.Account, req.Symbol, req.Leverage, "")
	}
	respondAdapterResult(c, result, err)
}

func (s *Server) setAdapterMarginMode(c *gin.Context) {
	adapter, ok := s.requireExchangeAdapter(c)
	if !ok {
		return
	}

	var req setAdapterMarginModeRequest
	if !bindAdapterJSON(c, &req) {
		return
	}
	result, err := adapter.SetMarginMode(c.Request.Context(), exchange.SetMarginModeRequest{
		AccountConfig: req.Account.toAccountConfig(),
		Symbol:        strings.TrimSpace(req.Symbol),
		MarginMode:    strings.TrimSpace(req.MarginMode),
		Params:        req.Params,
	})
	if err == nil && result.Success {
		s.schedulePositionSettingVerification(req.Account, req.Symbol, 0, req.MarginMode)
	}
	respondAdapterResult(c, result, err)
}

func (s *Server) scheduleOrderReconciliation(account exchangeAdapterAccountRequest, result exchange.OrderResult, symbol, fallbackOrderID, fallbackClientOrderID string) {
	if s.queue == nil || s.reconciliation == nil {
		return
	}
	accountID, err := s.resolveReconciliationAccountID(account)
	if err != nil {
		s.reconciliation.RecordScheduleFailure(context.Background(), "order", symbol, err)
		return
	}
	orderID := strings.TrimSpace(result.ExchangeOrderID)
	if orderID == "" {
		orderID = strings.TrimSpace(fallbackOrderID)
	}
	clientOrderID := strings.TrimSpace(result.ClientOrderID)
	if clientOrderID == "" {
		clientOrderID = strings.TrimSpace(fallbackClientOrderID)
	}
	if orderID == "" && clientOrderID == "" {
		return
	}
	if err := s.queue.ScheduleExternalOrderReconciliation(context.Background(), service.ExternalOrderReconciliationInput{
		AccountID: accountID, OrderID: orderID, ClientOrderID: clientOrderID, Symbol: strings.TrimSpace(symbol),
	}); err != nil {
		s.reconciliation.RecordScheduleFailure(context.Background(), "order", symbol, err)
	}
}

func (s *Server) schedulePositionSettingVerification(account exchangeAdapterAccountRequest, symbol string, leverage int, marginMode string) {
	if s.queue == nil || s.reconciliation == nil {
		return
	}
	accountID, err := s.resolveReconciliationAccountID(account)
	if err != nil {
		s.reconciliation.RecordScheduleFailure(context.Background(), "position_setting", symbol, err)
		return
	}
	if leverage > 0 {
		if err := s.queue.ScheduleLeverageVerification(context.Background(), accountID, strings.TrimSpace(symbol), leverage); err != nil {
			s.reconciliation.RecordScheduleFailure(context.Background(), "set_leverage", symbol, err)
		}
		return
	}
	if err := s.queue.ScheduleMarginModeVerification(context.Background(), accountID, strings.TrimSpace(symbol), strings.TrimSpace(marginMode)); err != nil {
		s.reconciliation.RecordScheduleFailure(context.Background(), "set_margin_mode", symbol, err)
	}
}

func (s *Server) resolveReconciliationAccountID(account exchangeAdapterAccountRequest) (uint, error) {
	return s.reconciliation.ResolveAccountID(context.Background(), service.ExchangeAccountIdentity{
		AccountID: account.ID, Exchange: account.Exchange, AccountName: account.AccountName,
		CCXTID: account.CCXTID, APIKey: account.APIKey,
	})
}

func (s *Server) requireExchangeAdapter(c *gin.Context) (exchange.Adapter, bool) {
	if s.adapter == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("exchange adapter is not configured"))
		return nil, false
	}
	return s.adapter, true
}

func bindAdapterJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return false
	}
	return true
}

func respondAdapterResult(c *gin.Context, result any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, result)
		return
	}

	status := http.StatusBadGateway
	if errors.Is(err, exchange.ErrOrderNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, exchange.ErrOrderAlreadyFinal) {
		status = http.StatusConflict
	}
	respondError(c, status, err)
}

func (req exchangeAdapterAccountRequest) toAccountConfig() exchange.AccountConfig {
	return exchange.AccountConfig{
		Exchange:      strings.TrimSpace(req.Exchange),
		AccountName:   strings.TrimSpace(req.AccountName),
		CCXTID:        strings.ToLower(strings.TrimSpace(req.CCXTID)),
		MarketType:    strings.ToLower(strings.TrimSpace(req.MarketType)),
		Sandbox:       req.Sandbox,
		DefaultSettle: strings.ToUpper(strings.TrimSpace(req.DefaultSettle)),
		AccountType:   strings.TrimSpace(req.AccountType),
		ProductType:   strings.TrimSpace(req.ProductType),
		Category:      strings.TrimSpace(req.Category),
		PositionMode:  strings.ToLower(strings.TrimSpace(req.PositionMode)),
		MarginMode:    strings.ToLower(strings.TrimSpace(req.MarginMode)),
		RecvWindowMS:  req.RecvWindowMS,
		RateLimitMS:   req.RateLimitMS,
		APIKey:        req.APIKey,
		APISecret:     req.APISecret,
		Passphrase:    req.Passphrase,
	}
}

func (req placeAdapterOrderRequest) toPlaceOrderRequest() exchange.PlaceOrderRequest {
	account := req.Account.toAccountConfig()
	return exchange.PlaceOrderRequest{
		ClientOrderID:       strings.TrimSpace(req.ClientOrderID),
		Exchange:            account.Exchange,
		AccountName:         account.AccountName,
		CCXTID:              account.CCXTID,
		MarketType:          account.MarketType,
		Sandbox:             account.Sandbox,
		DefaultSettle:       account.DefaultSettle,
		AccountType:         account.AccountType,
		ProductType:         account.ProductType,
		Category:            account.Category,
		PositionMode:        account.PositionMode,
		MarginMode:          account.MarginMode,
		RecvWindowMS:        account.RecvWindowMS,
		RateLimitMS:         account.RateLimitMS,
		APIKey:              account.APIKey,
		APISecret:           account.APISecret,
		Passphrase:          account.Passphrase,
		Symbol:              strings.TrimSpace(req.Symbol),
		Side:                strings.TrimSpace(req.Side),
		PositionSide:        strings.TrimSpace(req.PositionSide),
		OrderType:           strings.TrimSpace(req.OrderType),
		Quantity:            req.Quantity.Decimal,
		Price:               req.Price.Decimal,
		ReduceOnly:          req.ReduceOnly,
		ExchangeOrderParams: req.ExchangeOrderParams,
	}
}

func buildCloseAdapterOrderRequest(
	account exchangeAdapterAccountRequest,
	position exchange.Position,
	clientOrderID string,
	exchangeOrderParams map[string]any,
) exchange.PlaceOrderRequest {
	closeSide := "SELL"
	if position.Quantity.IsNegative() {
		closeSide = "BUY"
	}

	positionSide := "NET"
	if strings.EqualFold(account.PositionMode, "hedge") {
		positionSide = strings.ToUpper(strings.TrimSpace(position.PositionSide))
		if positionSide != "LONG" && positionSide != "SHORT" {
			if position.Quantity.IsNegative() {
				positionSide = "SHORT"
			} else {
				positionSide = "LONG"
			}
		}
	}

	return placeAdapterOrderRequest{
		Account:             account,
		ClientOrderID:       clientOrderID,
		Symbol:              position.Symbol,
		Side:                closeSide,
		PositionSide:        positionSide,
		OrderType:           "MARKET",
		Quantity:            adapterDecimal{Decimal: position.Quantity.Abs()},
		ReduceOnly:          true,
		ExchangeOrderParams: copyAdapterParams(exchangeOrderParams),
	}.toPlaceOrderRequest()
}

func copyAdapterParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(params))
	for key, value := range params {
		result[key] = value
	}
	return result
}

func (value *adapterDecimal) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" || raw == `""` {
		value.Decimal = decimal.Zero
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return err
		}
		if strings.TrimSpace(unquoted) == "" {
			value.Decimal = decimal.Zero
			return nil
		}
	}

	var parsed decimal.Decimal
	if err := parsed.UnmarshalJSON(data); err != nil {
		return err
	}
	value.Decimal = parsed
	return nil
}

func cleanStringSlice(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
