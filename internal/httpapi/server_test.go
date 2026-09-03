package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

func TestHealthz(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestBoolOrDefault(t *testing.T) {
	trueValue := true
	falseValue := false

	if boolOrDefault(nil, false) {
		t.Fatal("dry_run should default to false")
	}
	if !boolOrDefault(nil, true) {
		t.Fatal("enabled should default to true")
	}
	if !boolOrDefault(&trueValue, false) {
		t.Fatal("explicit true should be preserved")
	}
	if boolOrDefault(&falseValue, true) {
		t.Fatal("explicit false should be preserved")
	}
}

func TestIndexHTML(t *testing.T) {
	server := NewServer(nil, nil, nil)

	for _, path := range []string{"/", "/index.html"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		server.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s expected text/html, got %s", path, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), "交易所适配器测试台") {
			t.Fatalf("%s did not serve index.html", path)
		}
	}
}

func TestMetrics(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hedging_bot_http_requests_total") {
		t.Fatalf("metrics endpoint did not expose app metrics")
	}
}

func TestExposureSyncReturns503WhenNotConfigured(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exposures/sync", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPositionSyncReturns503WhenNotConfigured(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/positions/sync", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHedgeMonitorReturns503WhenNotConfigured(t *testing.T) {
	server := NewServer(nil, nil, nil)
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/hedge-monitor?agent_id=1", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/hedge-monitor/refresh", nil),
	}
	for _, request := range requests {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, request)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s expected 503, got %d: %s", request.URL.Path, rec.Code, rec.Body.String())
		}
	}
}

func TestExchangeAccountOptions(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/exchange-accounts/options", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"exchange":"Binance"`) || !strings.Contains(body, `"ccxt_id":"binanceusdm"`) {
		t.Fatalf("unexpected body %s", body)
	}
	for _, exchange := range []string{"Gate", "Hyperliquid", "Aster"} {
		if !strings.Contains(body, `"exchange":"`+exchange+`"`) {
			t.Fatalf("missing %s defaults in body %s", exchange, body)
		}
	}
}

func TestExchangeAdapterFetchBalance(t *testing.T) {
	server := NewServer(nil, nil, nil, exchange.NewSimulatedAdapter())
	body := `{
		"account": {
			"exchange": "Binance",
			"account_name": "Binance test",
			"ccxt_id": "binanceusdm",
			"market_type": "swap",
			"sandbox": true,
			"default_settle": "USDT",
			"position_mode": "one_way",
			"margin_mode": "cross"
		},
		"assets": ["USDT"],
		"params": {}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange-adapter/fetch-balance", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if !strings.Contains(response, `"Exchange":"Binance"`) || !strings.Contains(response, `"Asset":"USDT"`) {
		t.Fatalf("unexpected body %s", response)
	}
}

func TestExchangeAdapterFetchMarkets(t *testing.T) {
	server := NewServer(nil, nil, nil, exchange.NewSimulatedAdapter())
	body := `{
		"account": {
			"exchange": "Binance",
			"account_name": "Binance test",
			"ccxt_id": "binanceusdm",
			"market_type": "swap",
			"sandbox": true,
			"default_settle": "USDT",
			"position_mode": "one_way",
			"margin_mode": "cross"
		},
		"params": {}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange-adapter/fetch-markets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if !strings.Contains(response, `"Symbol":"BTC/USDT:USDT"`) || !strings.Contains(response, `"MarketType":"swap"`) {
		t.Fatalf("unexpected body %s", response)
	}
}

func TestExchangeAdapterReturns503WhenNotConfigured(t *testing.T) {
	server := NewServer(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange-adapter/fetch-balance", strings.NewReader(`{"account":{},"assets":["USDT"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "exchange adapter is not configured") {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestExchangeAdapterPlaceMarketOrderAllowsEmptyPrice(t *testing.T) {
	server := NewServer(nil, nil, nil, exchange.NewSimulatedAdapter())
	body := `{
		"account": {
			"exchange": "Binance",
			"account_name": "Binance test",
			"ccxt_id": "binanceusdm",
			"market_type": "swap",
			"sandbox": true,
			"default_settle": "USDT",
			"position_mode": "one_way",
			"margin_mode": "cross"
		},
		"client_order_id": "lab_test_market",
		"symbol": "BTC/USDT:USDT",
		"side": "BUY",
		"position_side": "LONG",
		"order_type": "MARKET",
		"quantity": "0.01",
		"price": "",
		"reduce_only": false,
		"exchange_order_params": {}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange-adapter/place-order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if !strings.Contains(response, `"Status":"filled"`) || !strings.Contains(response, `"Price":"0"`) {
		t.Fatalf("unexpected body %s", response)
	}
}

func TestExchangeAdapterClosePositionsRequiresConfirmation(t *testing.T) {
	server := NewServer(nil, nil, nil, exchange.NewSimulatedAdapter())
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/exchange-adapter/close-positions",
		strings.NewReader(`{"account":{"exchange":"Binance"},"confirm_close":false}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "confirm_close must be true") {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestExchangeAdapterClosePositionsClosesLongPosition(t *testing.T) {
	adapter := exchange.NewSimulatedAdapter()
	account := exchange.AccountConfig{
		Exchange:     "Binance",
		AccountName:  "Binance test",
		CCXTID:       "binanceusdm",
		MarketType:   "swap",
		Sandbox:      true,
		PositionMode: "one_way",
		MarginMode:   "cross",
	}
	_, err := adapter.PlaceOrder(context.Background(), exchange.PlaceOrderRequest{
		ClientOrderID: "seed_long_position",
		Exchange:      account.Exchange,
		AccountName:   account.AccountName,
		CCXTID:        account.CCXTID,
		MarketType:    account.MarketType,
		Sandbox:       account.Sandbox,
		PositionMode:  account.PositionMode,
		MarginMode:    account.MarginMode,
		Symbol:        "BTC/USDT:USDT",
		Side:          "BUY",
		PositionSide:  "NET",
		OrderType:     "LIMIT",
		Quantity:      decimal.RequireFromString("0.01"),
		Price:         decimal.RequireFromString("50000"),
	})
	if err != nil {
		t.Fatalf("seed position: %v", err)
	}

	server := NewServer(nil, nil, nil, adapter)
	body := `{
		"account": {
			"exchange": "Binance",
			"account_name": "Binance test",
			"ccxt_id": "binanceusdm",
			"market_type": "swap",
			"sandbox": true,
			"position_mode": "one_way",
			"margin_mode": "cross"
		},
		"symbols": ["BTC/USDT:USDT"],
		"params": {},
		"exchange_order_params": {},
		"confirm_close": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exchange-adapter/close-positions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response closeAdapterPositionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success || response.SuccessfulOrders != 1 || response.FailedOrders != 0 {
		t.Fatalf("unexpected response %+v", response)
	}
	if len(response.Items) != 1 || response.Items[0].Order == nil {
		t.Fatalf("expected one close order, got %+v", response.Items)
	}
	if response.Items[0].CloseSide != "SELL" || !response.Items[0].Order.ReduceOnly {
		t.Fatalf("unexpected close order %+v", response.Items[0])
	}

	positions, err := adapter.FetchPositions(context.Background(), exchange.FetchPositionsRequest{
		AccountConfig: account,
		Symbols:       []string{"BTC/USDT:USDT"},
	})
	if err != nil {
		t.Fatalf("fetch positions after close: %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("expected closed position, got %+v", positions)
	}
}

func TestBuildCloseAdapterOrderRequestUsesSafeCloseDirection(t *testing.T) {
	tests := []struct {
		name             string
		positionMode     string
		positionSide     string
		quantity         string
		wantSide         string
		wantPositionSide string
	}{
		{name: "one-way short", positionMode: "one_way", positionSide: "SHORT", quantity: "-0.02", wantSide: "BUY", wantPositionSide: "NET"},
		{name: "hedge short", positionMode: "hedge", positionSide: "SHORT", quantity: "-0.02", wantSide: "BUY", wantPositionSide: "SHORT"},
		{name: "hedge long", positionMode: "hedge", positionSide: "LONG", quantity: "0.02", wantSide: "SELL", wantPositionSide: "LONG"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := buildCloseAdapterOrderRequest(
				exchangeAdapterAccountRequest{Exchange: "Binance", PositionMode: test.positionMode},
				exchange.Position{
					Symbol:       "BTC/USDT:USDT",
					PositionSide: test.positionSide,
					Quantity:     decimal.RequireFromString(test.quantity),
				},
				"close_test",
				nil,
			)
			if order.Side != test.wantSide || order.PositionSide != test.wantPositionSide {
				t.Fatalf("unexpected direction side=%s position_side=%s", order.Side, order.PositionSide)
			}
			if order.OrderType != "MARKET" || !order.ReduceOnly || !order.Quantity.Equal(decimal.RequireFromString("0.02")) {
				t.Fatalf("unsafe close order %+v", order)
			}
		})
	}
}

func TestExchangeAccountResponseHidesSecrets(t *testing.T) {
	account := mgmt.ExchangeAccount{
		ID:                  1,
		Exchange:            "OKX",
		Name:                "main",
		CCXTID:              "okx",
		MarketType:          "swap",
		Sandbox:             true,
		DefaultSettle:       "USDT",
		PositionMode:        "one_way",
		MarginMode:          "cross",
		AllowedSymbols:      []byte(`["BTC/USDT:USDT"]`),
		APIKeyEncrypted:     "full-api-key",
		APISecretEncrypted:  "full-secret",
		PassphraseEncrypted: "full-passphrase",
		APIKeyHint:          "i-key",
		Metadata:            []byte(`{"note":"ok"}`),
	}

	resp := toExchangeAccountResponse(account)

	if !resp.APIKeySet || !resp.APISecretSet || !resp.PassphraseSet {
		t.Fatal("secret set flags should be true")
	}
	if resp.APIKeyHint != "i-key" {
		t.Fatalf("unexpected api key hint %s", resp.APIKeyHint)
	}
	if resp.CredentialStatus != "ready" {
		t.Fatalf("unexpected credential status %s", resp.CredentialStatus)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, secret := range []string{"full-api-key", "full-secret", "full-passphrase"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("response leaked secret %q: %s", secret, string(body))
		}
	}
}
