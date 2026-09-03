package exchange

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// OrderStatusSubmitted 表示交易所已接收订单，但订单还没有进入最终状态。
	OrderStatusSubmitted = "submitted"
	// OrderStatusFilled 表示订单已全部成交。
	OrderStatusFilled = "filled"
	// OrderStatusCanceled 表示订单在完成前已被撤销。
	OrderStatusCanceled = "canceled"
	// OrderStatusFailed 表示交易所拒单，或订单以错误状态结束。
	OrderStatusFailed = "failed"
	// OrderStatusDryRun 表示只生成了订单计划，没有真正发送到交易所。
	OrderStatusDryRun = "dry_run"
)

var (
	// ErrOrderNotFound 表示通过交易所订单号和客户端订单号都找不到订单。
	ErrOrderNotFound = errors.New("exchange order not found")
	// ErrOrderAlreadyFinal 表示订单已经成交、撤销或失败，不能再变更。
	ErrOrderAlreadyFinal = errors.New("exchange order is already final")
)

// AccountConfig 保存每次交易所适配器调用需要的账户配置。
type AccountConfig struct {
	// Exchange 是产品侧展示的交易所名称，例如 Binance 或 OKX。
	Exchange string
	// AccountName 是内部用于识别和选择账户的显示名称。
	AccountName string
	// CCXTID 是 CCXT 的交易所标识，例如 binanceusdm、bybit、bitget、okx 或 gate。
	CCXTID string
	// MarketType 是默认市场类型，常见值为 spot、swap 或 future。
	MarketType string
	// Sandbox 保留账户级测试环境标记；CCXT 实际环境由全局 EXCHANGE_ENV 控制。
	Sandbox bool
	// DefaultSettle 是默认结算币种，通常是 USDT 或 USDC。
	DefaultSettle string
	// AccountType 是交易所特有的账户类型，例如 unified 或 contract。
	AccountType string
	// ProductType 是交易所特有的产品类型，例如 USDT-FUTURES。
	ProductType string
	// Category 是交易所特有的交易分类，例如 linear。
	Category string
	// PositionMode 是持仓模式；交易所区分时通常为 one_way 或 hedge。
	PositionMode string
	// MarginMode 是保证金模式；交易所支持时通常为 cross 或 isolated。
	MarginMode string
	// RecvWindowMS 用于需要请求有效窗口的交易所，例如 Binance 的 recvWindow。
	RecvWindowMS int
	// RateLimitMS 在适配器支持时覆盖客户端限流间隔。
	RateLimitMS int
	// APIKey 是解密后的 API Key，只应在适配器边界内使用。
	APIKey string
	// APISecret 是解密后的 API Secret，只应在适配器边界内使用。
	APISecret string
	// Passphrase 是部分交易所需要的额外口令，例如 OKX 和 Bitget。
	Passphrase string
}

// FetchBalanceRequest 表示按币种查询账户余额的请求。
type FetchBalanceRequest struct {
	AccountConfig
	// Assets 用于限定查询的币种；为空时由适配器使用默认范围。
	Assets []string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// BalanceAsset 表示 FetchBalance 返回的单个币种余额。
type BalanceAsset struct {
	Asset string
	Free  decimal.Decimal
	Used  decimal.Decimal
	Total decimal.Decimal
	Debt  decimal.Decimal
	// UpdatedAt 优先记录交易所返回时间；没有时使用适配器当前时间。
	UpdatedAt time.Time
}

// BalanceResult 是归一化后的账户余额响应。
type BalanceResult struct {
	Exchange    string
	AccountName string
	Assets      []BalanceAsset
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// FetchPositionsRequest 表示查询当前合约持仓的请求。
type FetchPositionsRequest struct {
	AccountConfig
	// Symbols 用于限定查询的合约标的；为空时查询全部持仓。
	Symbols []string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// Position 是归一化后的交易所持仓快照。
type Position struct {
	Exchange    string
	AccountName string
	Symbol      string
	// PositionSide 表示持仓方向，通常为 NET、LONG 或 SHORT。
	PositionSide      string
	Quantity          decimal.Decimal
	NotionalUSDT      decimal.Decimal
	EntryPrice        decimal.Decimal
	MarkPrice         decimal.Decimal
	UnrealizedPnlUSDT decimal.Decimal
	Leverage          decimal.Decimal
	MarginMode        string
	// UpdatedAt 优先记录交易所返回时间；没有时使用适配器当前时间。
	UpdatedAt time.Time
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// FetchTickerRequest 表示查询单个交易标的最新行情价格的请求。
type FetchTickerRequest struct {
	AccountConfig
	Symbol string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// FetchMarketsRequest 表示查询交易所支持交易对的请求。
type FetchMarketsRequest struct {
	AccountConfig
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// Ticker 是归一化后的市场行情，用于对冲决策和价格校验。
type Ticker struct {
	Exchange string
	Symbol   string
	Last     decimal.Decimal
	Mark     decimal.Decimal
	Bid      decimal.Decimal
	Ask      decimal.Decimal
	BidSize  decimal.Decimal
	AskSize  decimal.Decimal
	// UpdatedAt 优先记录交易所返回时间；没有时使用适配器当前时间。
	UpdatedAt time.Time
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// Market 是归一化后的交易所交易对元数据。
type Market struct {
	Exchange        string
	CCXTID          string
	Symbol          string
	Base            string
	Quote           string
	Settle          string
	MarketType      string
	Active          bool
	Contract        bool
	Linear          bool
	Inverse         bool
	Spot            bool
	Swap            bool
	Future          bool
	Option          bool
	PricePrecision  decimal.Decimal
	AmountPrecision decimal.Decimal
	MinAmount       decimal.Decimal
	MinCost         decimal.Decimal
	ContractSize    decimal.Decimal
	CreatedAt       time.Time
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// PlaceOrderRequest 表示向选定交易所账户提交一笔对冲订单。
type PlaceOrderRequest struct {
	// ClientOrderID 由本服务生成，用于幂等下单和后续对账。
	ClientOrderID string
	Exchange      string
	AccountName   string
	CCXTID        string
	MarketType    string
	// Sandbox 保留账户级测试环境标记；CCXT 实际环境由全局 EXCHANGE_ENV 控制。
	Sandbox       bool
	DefaultSettle string
	AccountType   string
	ProductType   string
	Category      string
	PositionMode  string
	MarginMode    string
	RecvWindowMS  int
	RateLimitMS   int
	APIKey        string
	APISecret     string
	Passphrase    string
	Symbol        string
	Side          string
	PositionSide  string
	OrderType     string
	Quantity      decimal.Decimal
	// Price 是委托价格；非市价单必须传入。
	Price decimal.Decimal
	// ReduceOnly 在交易所支持时用于防止订单增加风险敞口。
	ReduceOnly bool
	// ExchangeOrderParams 承载交易所特有的下单参数。
	ExchangeOrderParams map[string]any
}

// FetchOrderRequest 表示按交易所订单号或客户端订单号查询订单。
type FetchOrderRequest struct {
	AccountConfig
	OrderID       string
	ClientOrderID string
	Symbol        string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// CancelOrderRequest 表示撤销一笔已提交但尚未终态的订单。
type CancelOrderRequest struct {
	AccountConfig
	OrderID       string
	ClientOrderID string
	Symbol        string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// OrderResult 是订单相关调用返回的归一化结果。
type OrderResult struct {
	ExchangeOrderID string
	ClientOrderID   string
	Status          string
	Symbol          string
	Side            string
	OrderType       string
	Quantity        decimal.Decimal
	Price           decimal.Decimal
	ReduceOnly      bool
	FilledQuantity  decimal.Decimal
	AvgPrice        decimal.Decimal
	FeeUSDT         decimal.Decimal
	// UpdatedAt 优先记录交易所返回时间；没有时使用适配器当前时间。
	UpdatedAt time.Time
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// SetLeverageRequest 表示为单个合约标的设置杠杆倍数。
type SetLeverageRequest struct {
	AccountConfig
	Symbol   string
	Leverage int
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// SetMarginModeRequest 表示切换单个合约标的的全仓/逐仓模式。
type SetMarginModeRequest struct {
	AccountConfig
	Symbol     string
	MarginMode string
	// Params 承载通用模型之外的交易所特有参数。
	Params map[string]any
}

// CommandResult 是交易所账户类命令的归一化执行结果。
type CommandResult struct {
	Success bool
	// Raw 保留原始交易所响应字段，便于排查问题和审计。
	Raw map[string]any
}

// Adapter 定义对冲执行和对账需要的交易所操作。
type Adapter interface {
	// FetchBalance 查询账户可用资金，用于风控检查或账户诊断。
	FetchBalance(ctx context.Context, req FetchBalanceRequest) (BalanceResult, error)
	// FetchPositions 查询交易所当前持仓，用于敞口对账。
	FetchPositions(ctx context.Context, req FetchPositionsRequest) ([]Position, error)
	// FetchTicker 查询当前行情价格，用于订单定价和滑点校验。
	FetchTicker(ctx context.Context, req FetchTickerRequest) (Ticker, error)
	// FetchMarkets 查询交易所支持的交易对列表，用于同步可交易标的配置。
	FetchMarkets(ctx context.Context, req FetchMarketsRequest) ([]Market, error)
	// PlaceOrder 提交一笔对冲订单。
	PlaceOrder(ctx context.Context, req PlaceOrderRequest) (OrderResult, error)
	// FetchOrder 查询一笔订单的最新状态。
	FetchOrder(ctx context.Context, req FetchOrderRequest) (OrderResult, error)
	// CancelOrder 撤销一笔尚未进入终态的订单。
	CancelOrder(ctx context.Context, req CancelOrderRequest) (OrderResult, error)
	// SetLeverage 为单个合约标的配置杠杆倍数。
	SetLeverage(ctx context.Context, req SetLeverageRequest) (CommandResult, error)
	// SetMarginMode 为单个合约标的配置全仓或逐仓模式。
	SetMarginMode(ctx context.Context, req SetMarginModeRequest) (CommandResult, error)
}
