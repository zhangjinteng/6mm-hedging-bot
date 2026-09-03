package mgmt

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ExchangeAccount struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	AgentID             uint64         `gorm:"column:agent_id" json:"agent_id"`
	Exchange            string         `gorm:"column:exchange" json:"exchange"`
	Name                string         `gorm:"column:name" json:"name"`
	CCXTID              string         `gorm:"column:ccxt_id" json:"ccxt_id"`
	MarketType          string         `gorm:"column:market_type" json:"market_type"`
	Sandbox             bool           `gorm:"column:sandbox" json:"sandbox"`
	DefaultSettle       string         `gorm:"column:default_settle" json:"default_settle"`
	AccountType         string         `gorm:"column:account_type" json:"account_type"`
	ProductType         string         `gorm:"column:product_type" json:"product_type"`
	Category            string         `gorm:"column:category" json:"category"`
	PositionMode        string         `gorm:"column:position_mode" json:"position_mode"`
	MarginMode          string         `gorm:"column:margin_mode" json:"margin_mode"`
	RecvWindowMS        int            `gorm:"column:recv_window_ms" json:"recv_window_ms"`
	RateLimitMS         int            `gorm:"column:rate_limit_ms" json:"rate_limit_ms"`
	AllowedSymbols      datatypes.JSON `gorm:"column:allowed_symbols" json:"allowed_symbols"`
	APIKeyEncrypted     string         `gorm:"column:api_key_encrypted" json:"-"`
	APIKeyHint          string         `gorm:"column:api_key_hint" json:"api_key_hint"`
	APISecretEncrypted  string         `gorm:"column:api_secret_encrypted" json:"-"`
	PassphraseEncrypted string         `gorm:"column:passphrase_encrypted" json:"-"`
	Status              string         `gorm:"column:status" json:"status"`
	IsPrimary           bool           `gorm:"column:is_primary" json:"is_primary"`
	Metadata            datatypes.JSON `gorm:"column:metadata" json:"metadata"`
	CreatedAt           time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (ExchangeAccount) TableName() string {
	return "exchange_accounts"
}

type ExchangeMarket struct {
	ID               uint            `gorm:"primaryKey" json:"id"`
	Exchange         string          `gorm:"column:exchange" json:"exchange"`
	CCXTID           string          `gorm:"column:ccxt_id" json:"ccxt_id"`
	ExchangeEnv      string          `gorm:"column:exchange_env" json:"exchange_env"`
	MarketType       string          `gorm:"column:market_type" json:"market_type"`
	Symbol           string          `gorm:"column:symbol" json:"symbol"`
	NormalizedSymbol string          `gorm:"column:normalized_symbol" json:"normalized_symbol"`
	BaseAsset        string          `gorm:"column:base_asset" json:"base_asset"`
	QuoteAsset       string          `gorm:"column:quote_asset" json:"quote_asset"`
	SettleAsset      string          `gorm:"column:settle_asset" json:"settle_asset"`
	Active           bool            `gorm:"column:active" json:"active"`
	Contract         bool            `gorm:"column:contract" json:"contract"`
	Linear           bool            `gorm:"column:linear" json:"linear"`
	Inverse          bool            `gorm:"column:inverse" json:"inverse"`
	Spot             bool            `gorm:"column:spot" json:"spot"`
	Swap             bool            `gorm:"column:swap" json:"swap"`
	Future           bool            `gorm:"column:future" json:"future"`
	Option           bool            `gorm:"column:option" json:"option"`
	PricePrecision   decimal.Decimal `gorm:"column:price_precision;type:numeric(38,18)" json:"price_precision"`
	AmountPrecision  decimal.Decimal `gorm:"column:amount_precision;type:numeric(38,18)" json:"amount_precision"`
	MinAmount        decimal.Decimal `gorm:"column:min_amount;type:numeric(38,18)" json:"min_amount"`
	MinCost          decimal.Decimal `gorm:"column:min_cost;type:numeric(38,18)" json:"min_cost"`
	ContractSize     decimal.Decimal `gorm:"column:contract_size;type:numeric(38,18)" json:"contract_size"`
	RawResponse      datatypes.JSON  `gorm:"column:raw_response" json:"raw_response"`
	FetchedAt        time.Time       `gorm:"column:fetched_at" json:"fetched_at"`
	CreatedAt        time.Time       `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time       `gorm:"column:updated_at" json:"updated_at"`
}

func (ExchangeMarket) TableName() string {
	return "exchange_markets"
}

type HedgeConfig struct {
	ID                uint            `gorm:"primaryKey" json:"id"`
	AgentID           uint64          `gorm:"column:agent_id" json:"agent_id"`
	ExchangeAccountID uint            `gorm:"column:exchange_account_id" json:"exchange_account_id"`
	ExchangeAccount   ExchangeAccount `gorm:"foreignKey:ExchangeAccountID" json:"exchange_account,omitempty"`
	Source            string          `gorm:"column:source" json:"source"`
	Symbol            string          `gorm:"column:symbol" json:"symbol"`
	TargetSymbol      string          `gorm:"column:target_symbol" json:"target_symbol"`
	TargetHedgeRatio  decimal.Decimal `gorm:"column:target_hedge_ratio" json:"target_hedge_ratio"`
	FirstTriggerUSDT  decimal.Decimal `gorm:"column:first_trigger_usdt" json:"first_trigger_usdt"`
	RebalanceUSDT     decimal.Decimal `gorm:"column:rebalance_usdt" json:"rebalance_usdt"`
	ExitUSDT          decimal.Decimal `gorm:"column:exit_usdt" json:"exit_usdt"`
	MaxSlippageBps    int             `gorm:"column:max_slippage_bps" json:"max_slippage_bps"`
	MaxNotionalUSDT   decimal.Decimal `gorm:"column:max_notional_usdt" json:"max_notional_usdt"`
	MinOrderUSDT      decimal.Decimal `gorm:"column:min_order_usdt" json:"min_order_usdt"`
	Leverage          int             `gorm:"column:leverage" json:"leverage"`
	Enabled           bool            `gorm:"column:enabled" json:"enabled"`
	LifecycleStatus   string          `gorm:"column:lifecycle_status" json:"lifecycle_status"`
	DryRun            bool            `gorm:"column:dry_run" json:"dry_run"`
	Metadata          datatypes.JSON  `gorm:"column:metadata" json:"metadata"`
	CreatedAt         time.Time       `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time       `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt         gorm.DeletedAt  `gorm:"column:deleted_at;index" json:"-"`
}

const (
	HedgeLifecycleActive      = "active"
	HedgeLifecycleClosing     = "closing"
	HedgeLifecycleCloseFailed = "close_failed"
	HedgeLifecycleDisabled    = "disabled"

	HedgeCloseRequested = "requested"
	HedgeCloseSubmitted = "submitted"
	HedgeCloseVerifying = "verifying"
	HedgeCloseCompleted = "completed"
	HedgeCloseFailed    = "failed"
)

type HedgeCloseRequest struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	ConfigID         uint       `gorm:"column:config_id" json:"config_id"`
	OrderExecutionID *int64     `gorm:"column:order_execution_id" json:"order_execution_id,omitempty"`
	IdempotencyKey   string     `gorm:"column:idempotency_key" json:"idempotency_key"`
	Status           string     `gorm:"column:status" json:"status"`
	ErrorMessage     string     `gorm:"column:error_message" json:"error_message"`
	RequestedAt      time.Time  `gorm:"column:requested_at" json:"requested_at"`
	CompletedAt      *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (HedgeCloseRequest) TableName() string {
	return "hedge_close_requests"
}

type HedgingSetting struct {
	AgentID   uint64    `gorm:"column:agent_id;primaryKey" json:"agent_id"`
	Enabled   bool      `gorm:"column:enabled" json:"enabled"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// HedgeMonitorConfig 把币种配置、代理商总开关和交易所账户组合成监控计算输入。
// 即使币种或总开关关闭也会返回，以保证关闭对冲后仍能持续展示风险敞口。
type HedgeMonitorConfig struct {
	Config        HedgeConfig
	GlobalEnabled bool
}

func (config HedgeMonitorConfig) CanExecute() bool {
	return config.GlobalEnabled &&
		config.Config.Enabled &&
		(config.Config.LifecycleStatus == "" || config.Config.LifecycleStatus == HedgeLifecycleActive) &&
		config.Config.AgentID != 0 &&
		config.Config.ExchangeAccount.ID != 0 &&
		config.Config.ExchangeAccount.AgentID == config.Config.AgentID &&
		config.Config.ExchangeAccount.Status == ExchangeAccountStatusActive
}

func (HedgingSetting) TableName() string {
	return "hedging_settings"
}

func (HedgeConfig) TableName() string {
	return "hedge_configs"
}
