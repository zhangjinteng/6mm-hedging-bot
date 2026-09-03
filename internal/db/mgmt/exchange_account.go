package mgmt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gorm.io/datatypes"
)

const (
	ExchangeAccountStatusActive   = "active"
	ExchangeAccountStatusDisabled = "disabled"

	MarketTypeSpot   = "spot"
	MarketTypeMargin = "margin"
	MarketTypeSwap   = "swap"
	MarketTypeFuture = "future"

	PositionModeOneWay = "one_way"
	PositionModeHedge  = "hedge"

	MarginModeCross    = "cross"
	MarginModeIsolated = "isolated"
)

type ExchangeDefaults struct {
	Exchange           string   `json:"exchange"`
	CCXTID             string   `json:"ccxt_id"`
	MarketType         string   `json:"market_type"`
	DefaultSettle      string   `json:"default_settle"`
	AccountType        string   `json:"account_type"`
	ProductType        string   `json:"product_type"`
	Category           string   `json:"category"`
	PositionMode       string   `json:"position_mode"`
	MarginMode         string   `json:"margin_mode"`
	RecvWindowMS       int      `json:"recv_window_ms"`
	RateLimitMS        int      `json:"rate_limit_ms"`
	PassphraseRequired bool     `json:"passphrase_required"`
	SupportedCCXTIDs   []string `json:"supported_ccxt_ids"`
}

type ExchangeAccountPatch struct {
	Exchange            *string
	Name                *string
	CCXTID              *string
	MarketType          *string
	Sandbox             *bool
	DefaultSettle       *string
	AccountType         *string
	ProductType         *string
	Category            *string
	PositionMode        *string
	MarginMode          *string
	RecvWindowMS        *int
	RateLimitMS         *int
	AllowedSymbols      *datatypes.JSON
	APIKeyEncrypted     *string
	APIKeyHint          *string
	APISecretEncrypted  *string
	PassphraseEncrypted *string
	Status              *string
	IsPrimary           *bool
	Metadata            *datatypes.JSON
}

var exchangeDefaultsByKey = map[string]ExchangeDefaults{
	"binance": {
		Exchange:         "Binance",
		CCXTID:           "binanceusdm",
		MarketType:       MarketTypeSwap,
		DefaultSettle:    "USDT",
		Category:         "linear",
		PositionMode:     PositionModeOneWay,
		MarginMode:       MarginModeCross,
		RecvWindowMS:     5000,
		SupportedCCXTIDs: []string{"binance", "binanceusdm", "binancecoinm"},
	},
	"bybit": {
		Exchange:         "Bybit",
		CCXTID:           "bybit",
		MarketType:       MarketTypeSwap,
		DefaultSettle:    "USDT",
		AccountType:      "unified",
		Category:         "linear",
		PositionMode:     PositionModeOneWay,
		MarginMode:       MarginModeCross,
		RecvWindowMS:     5000,
		SupportedCCXTIDs: []string{"bybit"},
	},
	"gate": {
		Exchange:         "Gate",
		CCXTID:           "gate",
		MarketType:       MarketTypeSwap,
		DefaultSettle:    "USDT",
		PositionMode:     PositionModeOneWay,
		MarginMode:       MarginModeCross,
		RecvWindowMS:     5000,
		SupportedCCXTIDs: []string{"gate"},
	},
	"hyperliquid": {
		Exchange:         "Hyperliquid",
		CCXTID:           "hyperliquid",
		MarketType:       MarketTypeSwap,
		DefaultSettle:    "USDC",
		PositionMode:     PositionModeOneWay,
		MarginMode:       MarginModeCross,
		RecvWindowMS:     5000,
		SupportedCCXTIDs: []string{"hyperliquid"},
	},
	"aster": {
		Exchange:         "Aster",
		CCXTID:           "aster",
		MarketType:       MarketTypeSwap,
		DefaultSettle:    "USDT",
		Category:         "linear",
		PositionMode:     PositionModeOneWay,
		MarginMode:       MarginModeCross,
		RecvWindowMS:     5000,
		SupportedCCXTIDs: []string{"aster"},
	},
	"bitget": {
		Exchange:           "Bitget",
		CCXTID:             "bitget",
		MarketType:         MarketTypeSwap,
		DefaultSettle:      "USDT",
		ProductType:        "USDT-FUTURES",
		PositionMode:       PositionModeOneWay,
		MarginMode:         MarginModeCross,
		RecvWindowMS:       5000,
		PassphraseRequired: true,
		SupportedCCXTIDs:   []string{"bitget"},
	},
	"okx": {
		Exchange:           "OKX",
		CCXTID:             "okx",
		MarketType:         MarketTypeSwap,
		DefaultSettle:      "USDT",
		PositionMode:       PositionModeOneWay,
		MarginMode:         MarginModeCross,
		RecvWindowMS:       5000,
		PassphraseRequired: true,
		SupportedCCXTIDs:   []string{"okx"},
	},
}

var exchangeKeyByCCXTID = buildExchangeKeyByCCXTID()

func SupportedExchangeDefaults() []ExchangeDefaults {
	keys := make([]string, 0, len(exchangeDefaultsByKey))
	for key := range exchangeDefaultsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	defaults := make([]ExchangeDefaults, 0, len(keys))
	for _, key := range keys {
		defaults = append(defaults, exchangeDefaultsByKey[key])
	}
	return defaults
}

func PrepareExchangeAccountForSave(account *ExchangeAccount) error {
	trimExchangeAccount(account)

	defaults, err := defaultsForAccount(account.Exchange, account.CCXTID)
	if err != nil {
		return err
	}
	account.Exchange = defaults.Exchange

	if account.CCXTID == "" {
		account.CCXTID = defaults.CCXTID
	} else {
		account.CCXTID = strings.ToLower(account.CCXTID)
	}
	if !contains(defaults.SupportedCCXTIDs, account.CCXTID) {
		return fmt.Errorf("%w: unsupported ccxt_id %q for %s", ErrInvalidConfig, account.CCXTID, defaults.Exchange)
	}

	if account.MarketType == "" {
		account.MarketType = defaults.MarketType
	}
	if account.DefaultSettle == "" {
		account.DefaultSettle = defaults.DefaultSettle
	}
	if account.AccountType == "" {
		account.AccountType = defaults.AccountType
	}
	if account.ProductType == "" {
		account.ProductType = defaults.ProductType
	}
	if account.Category == "" {
		account.Category = defaults.Category
	}
	if account.PositionMode == "" {
		account.PositionMode = defaults.PositionMode
	}
	if account.MarginMode == "" {
		account.MarginMode = defaults.MarginMode
	}
	if account.RecvWindowMS == 0 {
		account.RecvWindowMS = defaults.RecvWindowMS
	}
	if account.Status == "" {
		account.Status = ExchangeAccountStatusActive
	}
	if len(account.AllowedSymbols) == 0 {
		account.AllowedSymbols = []byte("[]")
	}
	if len(account.Metadata) == 0 {
		account.Metadata = []byte("{}")
	}
	if account.APIKeyHint == "" {
		account.APIKeyHint = lastVisible(account.APIKeyEncrypted, 4)
	}

	account.MarketType = strings.ToLower(account.MarketType)
	account.DefaultSettle = strings.ToUpper(account.DefaultSettle)
	account.PositionMode = strings.ToLower(account.PositionMode)
	account.MarginMode = strings.ToLower(account.MarginMode)

	if !contains([]string{MarketTypeSpot, MarketTypeMargin, MarketTypeSwap, MarketTypeFuture}, account.MarketType) {
		return fmt.Errorf("%w: market_type must be spot, margin, swap, or future", ErrInvalidConfig)
	}
	if !contains([]string{PositionModeOneWay, PositionModeHedge}, account.PositionMode) {
		return fmt.Errorf("%w: position_mode must be one_way or hedge", ErrInvalidConfig)
	}
	if !contains([]string{MarginModeCross, MarginModeIsolated}, account.MarginMode) {
		return fmt.Errorf("%w: margin_mode must be cross or isolated", ErrInvalidConfig)
	}
	if !contains([]string{ExchangeAccountStatusActive, ExchangeAccountStatusDisabled}, account.Status) {
		return fmt.Errorf("%w: status must be active or disabled", ErrInvalidConfig)
	}
	if account.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidConfig)
	}
	if account.RecvWindowMS < 0 || account.RecvWindowMS > 60000 {
		return fmt.Errorf("%w: recv_window_ms must be between 0 and 60000", ErrInvalidConfig)
	}
	if account.RateLimitMS < 0 || account.RateLimitMS > 60000 {
		return fmt.Errorf("%w: rate_limit_ms must be between 0 and 60000", ErrInvalidConfig)
	}
	if err := validateJSONArray(account.AllowedSymbols, "allowed_symbols"); err != nil {
		return err
	}
	if err := validateJSONObject(account.Metadata, "metadata"); err != nil {
		return err
	}
	return nil
}

func ApplyExchangeAccountPatch(account *ExchangeAccount, patch ExchangeAccountPatch) error {
	if patch.Exchange != nil {
		account.Exchange = *patch.Exchange
	}
	if patch.Name != nil {
		account.Name = *patch.Name
	}
	if patch.CCXTID != nil {
		account.CCXTID = *patch.CCXTID
	}
	if patch.MarketType != nil {
		account.MarketType = *patch.MarketType
	}
	if patch.Sandbox != nil {
		account.Sandbox = *patch.Sandbox
	}
	if patch.DefaultSettle != nil {
		account.DefaultSettle = *patch.DefaultSettle
	}
	if patch.AccountType != nil {
		account.AccountType = *patch.AccountType
	}
	if patch.ProductType != nil {
		account.ProductType = *patch.ProductType
	}
	if patch.Category != nil {
		account.Category = *patch.Category
	}
	if patch.PositionMode != nil {
		account.PositionMode = *patch.PositionMode
	}
	if patch.MarginMode != nil {
		account.MarginMode = *patch.MarginMode
	}
	if patch.RecvWindowMS != nil {
		account.RecvWindowMS = *patch.RecvWindowMS
	}
	if patch.RateLimitMS != nil {
		account.RateLimitMS = *patch.RateLimitMS
	}
	if patch.AllowedSymbols != nil {
		account.AllowedSymbols = *patch.AllowedSymbols
	}
	if patch.APIKeyEncrypted != nil && *patch.APIKeyEncrypted != "" {
		account.APIKeyEncrypted = *patch.APIKeyEncrypted
		if patch.APIKeyHint == nil {
			account.APIKeyHint = lastVisible(*patch.APIKeyEncrypted, 4)
		}
	}
	if patch.APIKeyHint != nil {
		account.APIKeyHint = *patch.APIKeyHint
	}
	if patch.APISecretEncrypted != nil && *patch.APISecretEncrypted != "" {
		account.APISecretEncrypted = *patch.APISecretEncrypted
	}
	if patch.PassphraseEncrypted != nil && *patch.PassphraseEncrypted != "" {
		account.PassphraseEncrypted = *patch.PassphraseEncrypted
	}
	if patch.Status != nil {
		account.Status = *patch.Status
	}
	if patch.IsPrimary != nil {
		account.IsPrimary = *patch.IsPrimary
	}
	if patch.Metadata != nil {
		account.Metadata = *patch.Metadata
	}
	return PrepareExchangeAccountForSave(account)
}

func PassphraseRequired(account ExchangeAccount) bool {
	defaults, err := defaultsForAccount(account.Exchange, account.CCXTID)
	if err != nil {
		return false
	}
	return defaults.PassphraseRequired
}

func CredentialStatus(account ExchangeAccount) string {
	if account.APIKeyEncrypted == "" || account.APISecretEncrypted == "" {
		return "incomplete"
	}
	if PassphraseRequired(account) && account.PassphraseEncrypted == "" {
		return "incomplete"
	}
	return "ready"
}

func BuildAllowedSymbolsJSON(symbols []string) (datatypes.JSON, error) {
	cleaned := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			return nil, fmt.Errorf("%w: allowed_symbols contains empty symbol", ErrInvalidConfig)
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		cleaned = append(cleaned, symbol)
	}
	body, err := json.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal allowed_symbols: %v", ErrInvalidConfig, err)
	}
	return datatypes.JSON(body), nil
}

func defaultsForAccount(exchange, ccxtID string) (ExchangeDefaults, error) {
	key := normalizeExchangeKey(exchange)
	if key == "" && ccxtID != "" {
		key = exchangeKeyByCCXTID[strings.ToLower(strings.TrimSpace(ccxtID))]
	}
	if key == "" {
		return ExchangeDefaults{}, fmt.Errorf("%w: exchange is required", ErrInvalidConfig)
	}
	defaults, ok := exchangeDefaultsByKey[key]
	if !ok {
		return ExchangeDefaults{}, fmt.Errorf("%w: unsupported exchange %q", ErrInvalidConfig, exchange)
	}
	return defaults, nil
}

func buildExchangeKeyByCCXTID() map[string]string {
	keys := make(map[string]string)
	for key, defaults := range exchangeDefaultsByKey {
		for _, ccxtID := range defaults.SupportedCCXTIDs {
			keys[ccxtID] = key
		}
	}
	return keys
}

func normalizeExchangeKey(exchange string) string {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	exchange = strings.ReplaceAll(exchange, "-", "")
	exchange = strings.ReplaceAll(exchange, "_", "")
	exchange = strings.ReplaceAll(exchange, " ", "")
	exchange = strings.ReplaceAll(exchange, ".", "")
	switch exchange {
	case "okex":
		return "okx"
	case "binanceusdm", "binancecoinm":
		return "binance"
	case "gateio":
		return "gate"
	case "asterdex":
		return "aster"
	default:
		return exchange
	}
}

func trimExchangeAccount(account *ExchangeAccount) {
	account.Exchange = strings.TrimSpace(account.Exchange)
	account.Name = strings.TrimSpace(account.Name)
	account.CCXTID = strings.TrimSpace(account.CCXTID)
	account.MarketType = strings.TrimSpace(account.MarketType)
	account.DefaultSettle = strings.TrimSpace(account.DefaultSettle)
	account.AccountType = strings.TrimSpace(account.AccountType)
	account.ProductType = strings.TrimSpace(account.ProductType)
	account.Category = strings.TrimSpace(account.Category)
	account.PositionMode = strings.TrimSpace(account.PositionMode)
	account.MarginMode = strings.TrimSpace(account.MarginMode)
	account.APIKeyEncrypted = strings.TrimSpace(account.APIKeyEncrypted)
	account.APIKeyHint = strings.TrimSpace(account.APIKeyHint)
	account.APISecretEncrypted = strings.TrimSpace(account.APISecretEncrypted)
	account.PassphraseEncrypted = strings.TrimSpace(account.PassphraseEncrypted)
	account.Status = strings.TrimSpace(account.Status)
}

func validateJSONArray(raw datatypes.JSON, field string) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%w: %s must be valid JSON array", ErrInvalidConfig, field)
	}
	if _, ok := value.([]any); !ok {
		return fmt.Errorf("%w: %s must be JSON array", ErrInvalidConfig, field)
	}
	return nil
}

func validateJSONObject(raw datatypes.JSON, field string) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%w: %s must be valid JSON object", ErrInvalidConfig, field)
	}
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("%w: %s must be JSON object", ErrInvalidConfig, field)
	}
	return nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func lastVisible(value string, count int) string {
	if value == "" || count <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[len(runes)-count:])
}
