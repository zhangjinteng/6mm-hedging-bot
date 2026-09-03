CREATE TABLE IF NOT EXISTS exchange_markets (
    id bigserial PRIMARY KEY,
    exchange text NOT NULL,
    ccxt_id text NOT NULL DEFAULT '',
    exchange_env text NOT NULL DEFAULT 'paper',
    market_type text NOT NULL DEFAULT '',
    symbol text NOT NULL,
    base_asset text NOT NULL DEFAULT '',
    quote_asset text NOT NULL DEFAULT '',
    settle_asset text NOT NULL DEFAULT '',
    active boolean NOT NULL DEFAULT false,
    contract boolean NOT NULL DEFAULT false,
    linear boolean NOT NULL DEFAULT false,
    inverse boolean NOT NULL DEFAULT false,
    spot boolean NOT NULL DEFAULT false,
    swap boolean NOT NULL DEFAULT false,
    future boolean NOT NULL DEFAULT false,
    option boolean NOT NULL DEFAULT false,
    price_precision numeric(38, 18) NOT NULL DEFAULT 0,
    amount_precision numeric(38, 18) NOT NULL DEFAULT 0,
    min_amount numeric(38, 18) NOT NULL DEFAULT 0,
    min_cost numeric(38, 18) NOT NULL DEFAULT 0,
    contract_size numeric(38, 18) NOT NULL DEFAULT 0,
    raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exchange_markets_exchange_env_check CHECK (exchange_env IN ('paper', 'live')),
    CONSTRAINT exchange_markets_market_type_check CHECK (market_type IN ('', 'spot', 'margin', 'swap', 'future', 'option')),
    CONSTRAINT exchange_markets_symbol_check CHECK (length(btrim(symbol)) > 0),
    CONSTRAINT exchange_markets_raw_response_object_check CHECK (jsonb_typeof(raw_response) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS exchange_markets_identity_uidx
    ON exchange_markets (exchange, ccxt_id, exchange_env, market_type, settle_asset, symbol);

CREATE INDEX IF NOT EXISTS exchange_markets_exchange_symbol_idx
    ON exchange_markets (exchange, ccxt_id, exchange_env, symbol);

CREATE INDEX IF NOT EXISTS exchange_markets_active_idx
    ON exchange_markets (exchange, ccxt_id, exchange_env, active, symbol);

CREATE INDEX IF NOT EXISTS exchange_markets_fetched_at_idx
    ON exchange_markets (fetched_at DESC);

DROP TRIGGER IF EXISTS exchange_markets_set_updated_at ON exchange_markets;
CREATE TRIGGER exchange_markets_set_updated_at
BEFORE UPDATE ON exchange_markets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE exchange_markets IS '交易所支持交易对表，保存从 CCXT 或交易所接口同步到的可交易标的元数据。';
COMMENT ON COLUMN exchange_markets.exchange IS '交易所展示名称，例如 Binance、Bybit、Bitget 或 Gate。';
COMMENT ON COLUMN exchange_markets.ccxt_id IS 'CCXT 交易所标识，例如 binanceusdm、bybit、bitget 或 gate。';
COMMENT ON COLUMN exchange_markets.exchange_env IS '交易所环境：paper 测试网或模拟盘，live 正式环境。';
COMMENT ON COLUMN exchange_markets.market_type IS '市场类型：spot 现货、margin 杠杆、swap 永续合约、future 交割合约、option 期权。';
COMMENT ON COLUMN exchange_markets.symbol IS 'CCXT 统一交易对标识，例如 BTC/USDT:USDT。';
COMMENT ON COLUMN exchange_markets.base_asset IS '基础资产，例如 BTC。';
COMMENT ON COLUMN exchange_markets.quote_asset IS '报价资产，例如 USDT。';
COMMENT ON COLUMN exchange_markets.settle_asset IS '合约结算资产，例如 USDT 或 USDC。';
COMMENT ON COLUMN exchange_markets.active IS '交易所是否标记该交易对可交易。';
COMMENT ON COLUMN exchange_markets.contract IS '是否为合约市场。';
COMMENT ON COLUMN exchange_markets.linear IS '是否为线性合约。';
COMMENT ON COLUMN exchange_markets.inverse IS '是否为反向合约。';
COMMENT ON COLUMN exchange_markets.spot IS '是否为现货市场。';
COMMENT ON COLUMN exchange_markets.swap IS '是否为永续合约市场。';
COMMENT ON COLUMN exchange_markets.future IS '是否为交割合约市场。';
COMMENT ON COLUMN exchange_markets.option IS '是否为期权市场。';
COMMENT ON COLUMN exchange_markets.price_precision IS '价格精度。';
COMMENT ON COLUMN exchange_markets.amount_precision IS '数量精度。';
COMMENT ON COLUMN exchange_markets.min_amount IS '最小下单数量。';
COMMENT ON COLUMN exchange_markets.min_cost IS '最小下单名义价值。';
COMMENT ON COLUMN exchange_markets.contract_size IS '合约面值或合约乘数。';
COMMENT ON COLUMN exchange_markets.raw_response IS '原始交易所或 CCXT market 响应，使用 JSON 保存。';
COMMENT ON COLUMN exchange_markets.fetched_at IS '本服务同步到该交易对元数据的时间。';
COMMENT ON COLUMN exchange_markets.created_at IS '记录创建时间。';
COMMENT ON COLUMN exchange_markets.updated_at IS '记录最后更新时间。';
