package mgmt

import (
	"errors"
	"testing"
)

func TestPrepareExchangeAccountForSaveDefaultsBinance(t *testing.T) {
	account := ExchangeAccount{
		Exchange: "binance",
		Name:     " main ",
		Sandbox:  true,
	}

	if err := PrepareExchangeAccountForSave(&account); err != nil {
		t.Fatalf("prepare account: %v", err)
	}

	if account.Exchange != "Binance" {
		t.Fatalf("unexpected exchange %q", account.Exchange)
	}
	if account.CCXTID != "binanceusdm" {
		t.Fatalf("unexpected ccxt id %q", account.CCXTID)
	}
	if account.MarketType != MarketTypeSwap {
		t.Fatalf("unexpected market type %q", account.MarketType)
	}
	if account.DefaultSettle != "USDT" {
		t.Fatalf("unexpected settle %q", account.DefaultSettle)
	}
	if string(account.AllowedSymbols) != "[]" {
		t.Fatalf("unexpected allowed symbols %s", string(account.AllowedSymbols))
	}
	if string(account.Metadata) != "{}" {
		t.Fatalf("unexpected metadata %s", string(account.Metadata))
	}
}

func TestSupportedExchangeDefaultsIncludeNewExchanges(t *testing.T) {
	defaults := SupportedExchangeDefaults()
	found := map[string]ExchangeDefaults{}
	for _, item := range defaults {
		found[item.Exchange] = item
	}

	tests := []struct {
		exchange      string
		ccxtID        string
		defaultSettle string
	}{
		{exchange: "Gate", ccxtID: "gate", defaultSettle: "USDT"},
		{exchange: "Hyperliquid", ccxtID: "hyperliquid", defaultSettle: "USDC"},
		{exchange: "Aster", ccxtID: "aster", defaultSettle: "USDT"},
	}
	for _, tt := range tests {
		got, ok := found[tt.exchange]
		if !ok {
			t.Fatalf("missing defaults for %s", tt.exchange)
		}
		if got.CCXTID != tt.ccxtID {
			t.Fatalf("%s ccxt id: expected %s, got %s", tt.exchange, tt.ccxtID, got.CCXTID)
		}
		if got.DefaultSettle != tt.defaultSettle {
			t.Fatalf("%s settle: expected %s, got %s", tt.exchange, tt.defaultSettle, got.DefaultSettle)
		}
		if got.MarketType != MarketTypeSwap {
			t.Fatalf("%s market type: expected swap, got %s", tt.exchange, got.MarketType)
		}
	}
}

func TestPrepareExchangeAccountForSaveNormalizesNewExchangeAliases(t *testing.T) {
	tests := []struct {
		exchange string
		wantName string
		wantID   string
	}{
		{exchange: "gate.io", wantName: "Gate", wantID: "gate"},
		{exchange: "asterdex", wantName: "Aster", wantID: "aster"},
		{exchange: "hyperliquid", wantName: "Hyperliquid", wantID: "hyperliquid"},
	}
	for _, tt := range tests {
		account := ExchangeAccount{
			Exchange: tt.exchange,
			Name:     "main",
		}

		if err := PrepareExchangeAccountForSave(&account); err != nil {
			t.Fatalf("prepare %s: %v", tt.exchange, err)
		}
		if account.Exchange != tt.wantName {
			t.Fatalf("%s exchange: expected %s, got %s", tt.exchange, tt.wantName, account.Exchange)
		}
		if account.CCXTID != tt.wantID {
			t.Fatalf("%s ccxt id: expected %s, got %s", tt.exchange, tt.wantID, account.CCXTID)
		}
	}
}

func TestPrepareExchangeAccountForSaveRejectsUnsupportedExchange(t *testing.T) {
	account := ExchangeAccount{
		Exchange: "unsupported",
		Name:     "main",
	}

	err := PrepareExchangeAccountForSave(&account)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
}

func TestCredentialStatusRequiresPassphraseForOKX(t *testing.T) {
	account := ExchangeAccount{
		Exchange:           "OKX",
		Name:               "main",
		APIKeyEncrypted:    "key",
		APISecretEncrypted: "secret",
	}
	if err := PrepareExchangeAccountForSave(&account); err != nil {
		t.Fatalf("prepare account: %v", err)
	}

	if !PassphraseRequired(account) {
		t.Fatal("OKX should require passphrase")
	}
	if status := CredentialStatus(account); status != "incomplete" {
		t.Fatalf("expected incomplete, got %s", status)
	}

	account.PassphraseEncrypted = "passphrase"
	if status := CredentialStatus(account); status != "ready" {
		t.Fatalf("expected ready, got %s", status)
	}
}

func TestBuildAllowedSymbolsJSONDeduplicates(t *testing.T) {
	body, err := BuildAllowedSymbolsJSON([]string{"BTC/USDT:USDT", "BTC/USDT:USDT", "ETH/USDT:USDT"})
	if err != nil {
		t.Fatalf("build symbols: %v", err)
	}
	want := `["BTC/USDT:USDT","ETH/USDT:USDT"]`
	if string(body) != want {
		t.Fatalf("expected %s, got %s", want, string(body))
	}
}
