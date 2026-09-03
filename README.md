# 6mm-hedging-bot

Go service for 6MM contract hedging.

The first version focuses on the backend engine and PostgreSQL storage:

- PostgreSQL as the primary database.
- `sqlc + database/sql + pgx/stdlib` for core financial tables.
- GORM for management configuration tables.
- Decimal arithmetic for money and position sizes.
- Dry-run first execution flow with idempotent order plans.
- Exchange integration behind an adapter interface.
- gRPC for internal calls and HTTP REST for the admin backend.
- Redis distributed lock, Asynq task queue, and gocron scheduling.
- Prometheus metrics at `/metrics`.
- zerolog structured logs.

## Architecture

```text
Admin/API
  -> HedgeService
  -> HedgeCalculator
  -> Risk/order plan
  -> ExchangeAdapter
  -> PostgreSQL

Scheduler -> Asynq/Redis -> Worker -> HedgeService
MarketSyncScheduler -> ExchangeAdapter -> PostgreSQL exchange_markets
ExposureSyncScheduler -> ClickHouse current_position_query + Redis mark price
  -> PostgreSQL exposure_snapshots -> Asynq/Redis -> HedgeService
PositionSyncScheduler -> ExchangeAdapter -> PostgreSQL hedge_position_snapshots
HedgeMonitorService -> exposure + position + config + execution -> hedge_monitor_snapshots
```

The default exchange adapter is simulated. It lets the service create order
plans and execution records without touching real funds. The CCXT-backed
adapter bridge is kept isolated in `internal/exchange/ccxt_adapter.go` and
delegates exchange-specific compatibility to `github.com/zhangjinteng/6mm-ccxt`,
so real Binance, Bybit, Bitget, OKX, Gate, Hyperliquid, or Aster integration
can stay behind `internal/exchange.Adapter` without affecting the default build.

## Exchange Adapter Calls

`internal/exchange.Adapter` now covers the calls needed for account setup,
hedge execution, and reconciliation:

- `FetchBalance`: query account assets and available balance.
- `FetchPositions`: query current contract positions.
- `FetchTicker`: query latest mark/last/bid/ask prices.
- `FetchMarkets`: query supported exchange markets for symbol synchronization.
- `PlaceOrder`: submit hedge orders with client order id and reduce-only flag.
- `FetchOrder`: query order state by exchange order id or client order id.
- `CancelOrder`: cancel submitted orders by exchange order id or client order id.
- `SetLeverage`: set leverage for one symbol.
- `SetMarginMode`: switch cross/isolated margin mode for one symbol.

The simulated adapter implements all calls locally for safe development. The
CCXT bridge adapter maps the same calls through `6mm-ccxt` and should still be
validated per exchange before live trading, especially sandbox URLs, contract
symbols, margin mode, position mode, `reduceOnly`, and passphrase behavior.

## Database

Core financial tables are created by migration SQL:

- `exposure_snapshots`
- `hedge_position_snapshots`
- `hedge_monitor_snapshots`
- `order_plans`
- `order_executions`
- `audit_events`

Management configuration tables are queried through GORM:

- `exchange_accounts`
- `exchange_markets`
- `hedge_configs`

## Run

Create a database and set `DATABASE_URL`:

```bash
cp .env.example .env
export DATABASE_URL='postgres://user:password@127.0.0.1:5432/platform_admin?sslmode=disable'
go run ./cmd/hedging-bot
```

By default the service applies migrations from `internal/db/migrations`.
Queue and scheduler features are enabled when `REDIS_ADDR` is configured.
Market synchronization does not require Redis; when `MARKET_SYNC_ENABLED=true`,
it periodically reads active `exchange_accounts`, queries supported markets
through the exchange adapter, and upserts them into `exchange_markets`. If a
symbol config database is configured, the sync first reads
`public.symbol_config` and only persists enabled symbols from that allowlist.
The sync also prunes historical rows from the same exchange source when they
do not match the account `market_type` or configured allowlist.
`exchange_markets.symbol` keeps the raw CCXT symbol, while
`exchange_markets.normalized_symbol` stores the platform symbol form such as
`BTCUSDT` for allowlist matching. The first sync runs on service startup, then
repeats by interval.

HTTP listens on `HTTP_ADDR`, gRPC listens on `GRPC_ADDR`. `EXCHANGE_ADAPTER`
defaults to `simulated`. `EXCHANGE_ENV` controls the real exchange
environment globally: `paper` means testnet/demo trading, and `live` means the
production exchange environment. The request/account `sandbox` field is kept
for compatibility and metadata; CCXT uses `EXCHANGE_ENV` as the final switch.
For Bitget `paper`, keep `product_type=USDT-FUTURES` and use API credentials
created in the Bitget Demo/Paper environment. A live Bitget API key will be
rejected by Bitget with `40099 exchange environment is incorrect` when
`EXCHANGE_ENV=paper`.

Application and Asynq logs share the same output configuration:

```bash
LOG_OUTPUT=both
LOG_FILE=logs/hedging-bot.log
LOG_LEVEL=info
LOG_FORMAT=json
```

`LOG_OUTPUT` accepts `console`, `file`, or `both`. File output is appended and
its parent directory is created automatically. `LOG_FORMAT` accepts `json` or
`console`; `LOG_LEVEL` uses zerolog levels such as `debug`, `info`, `warn`, and
`error`. Relative log paths are resolved from the service working directory.

Market metadata can be synced on a fixed interval:

```bash
MARKET_SYNC_ENABLED=true
MARKET_SYNC_INTERVAL=6h
```

Platform exposure can be synchronized independently of the legacy hedge
scheduler. The sync aggregates active ClickHouse positions by
`agent_id + symbol`, reads `mark` and `ts_mark` from
`market_price:6mm:{symbol}`, writes an agent-scoped snapshot, and enqueues the
existing hedge runner. It does not open its own WebSocket connection.
Exposure collection scans all non-deleted hedge configurations, including
disabled symbols, so turning off execution does not hide existing risk. Only
configs with an enabled agent switch, enabled symbol, and active account are
submitted to the hedge queue.

```bash
EXPOSURE_SYNC_ENABLED=true
EXPOSURE_SYNC_INTERVAL=30s
EXPOSURE_PRICE_MAX_AGE=2m
HEDGE_RUN_LOCK_TTL=3m
HEDGE_RUN_COOLDOWN=10s
CLICKHOUSE_HISTORY_HOST=127.0.0.1
CLICKHOUSE_HISTORY_HTTP_PORT=8123
CLICKHOUSE_HISTORY_DATABASE=freedex_history
CLICKHOUSE_HISTORY_USERNAME=history_readonly
CLICKHOUSE_HISTORY_PASSWORD=change-me
```

`EXPOSURE_SYNC_ENABLED` does not depend on `SCHEDULER_ENABLED`. The first
exposure sync runs immediately at startup and subsequent runs use
`EXPOSURE_SYNC_INTERVAL`.

Automatic hedge runs are serialized per configuration with a Redis lock. The
lock remains held while a filled order refreshes its exchange position snapshot.
`HEDGE_RUN_LOCK_TTL` bounds stale locks, while `HEDGE_RUN_COOLDOWN` suppresses
new scheduled runs until the latest fill has had time to appear in position
snapshots. Submitted orders and fills newer than the latest position snapshot
are skipped rather than risk a duplicate order.

When a rebalance would reverse the exchange position, the calculator emits a
reduce-only close order first (`position_flip_close`). A later run opens the
opposite position only after the close is filled and the zero position snapshot
has passed the normal cooldown and synchronization guards.

Real exchange positions can be refreshed independently. Each active account is
queried once per cycle for all configured target symbols. Missing positions are
written as fresh zero snapshots, which prevents a closed position from leaving
a stale non-zero value in monitoring. `POSITION_SYNC_ENABLED` is independent of
`SCHEDULER_ENABLED`; the latter only controls the legacy hedge-config scheduler.

```bash
POSITION_SYNC_ENABLED=true
POSITION_SYNC_INTERVAL=10s
POSITION_STALE_AFTER=1m
```

The monitoring read model combines exposure, exchange position, configuration,
account health, and the latest execution result. It exposes the statuses
`global_off`, `symbol_off`, `account_unavailable`, `observing`,
`open_required`, `rebalance_required`, `exit_required`, `balanced`, and
`execution_failed`. Collection continues for disabled symbol configurations,
while only executable configurations are sent to the hedge queue.

To filter synced markets by the platform symbol configuration table, provide a
read-only connection to the database that contains `public.symbol_config`.
`SYMBOL_CONFIG_DATABASE_URL` takes priority; otherwise the service will build
the URL from the Laravel-style `DB_*` variables:

```bash
DB_CONNECTION=pgsql
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=freedex
DB_USERNAME=freedex_app
DB_PASSWORD=change-me
DB_CONNECT_TIMEOUT=5
```

The real CCXT adapter is isolated behind the `ccxt` build tag. When
`EXCHANGE_ADAPTER=ccxt` is set in `.env`, `make run` and `make build`
automatically add `-tags ccxt`:

```bash
make run
```

If you run Go directly instead of using `make`, include the tag yourself:

```bash
EXCHANGE_ADAPTER=ccxt go run -tags ccxt ./cmd/hedging-bot
```

## API examples

List supported exchange defaults for the account form:

```bash
curl http://127.0.0.1:7892/api/v1/exchange-accounts/options
```

Create an exchange account. Store encrypted key material, not raw API secrets.
`ccxt_id` defaults to `binanceusdm` for Binance, `bybit` for Bybit, `bitget`
for Bitget, `okx` for OKX, `gate` for Gate, `hyperliquid` for Hyperliquid,
and `aster` for Aster. OKX and Bitget require passphrase before the credential
status becomes `ready`. Hyperliquid uses `USDC` as the default settlement
currency.

```bash
curl -X POST http://127.0.0.1:7892/api/v1/exchange-accounts \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange": "Binance",
    "name": "Binance main",
    "market_type": "swap",
    "sandbox": true,
    "default_settle": "USDT",
    "position_mode": "one_way",
    "margin_mode": "cross",
    "allowed_symbols": ["BTC/USDT:USDT", "ETH/USDT:USDT"],
    "api_key_encrypted": "",
    "api_key_hint": "",
    "api_secret_encrypted": "",
    "is_primary": true
  }'
```

Update an exchange account. Empty secret fields are ignored, which supports
"leave blank to keep existing credential" forms:

```bash
curl -X PATCH http://127.0.0.1:7892/api/v1/exchange-accounts/1 \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Binance main account",
    "sandbox": false,
    "recv_window_ms": 5000
  }'
```

Mark one account as the current primary execution account:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/exchange-accounts/1/primary
```

Sync supported exchange markets into PostgreSQL:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/exchange-markets/sync \
  -H 'Content-Type: application/json' \
  -d '{
    "account": {
      "exchange": "Binance",
      "account_name": "Binance market sync",
      "ccxt_id": "binanceusdm",
      "market_type": "swap",
      "sandbox": true,
      "default_settle": "USDT",
      "category": "linear"
    },
    "params": {}
  }'
```

List synced markets:

```bash
curl 'http://127.0.0.1:7892/api/v1/exchange-markets?exchange=Binance&ccxt_id=binanceusdm&market_type=swap&settle_asset=USDT&limit=200'
```

List one platform-normalized symbol:

```bash
curl 'http://127.0.0.1:7892/api/v1/exchange-markets?normalized_symbol=BTCUSDT&limit=20'
```

Create a hedge config:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/hedge-configs \
  -H 'Content-Type: application/json' \
  -d '{
    "exchange_account_id": 1,
    "source": "platform",
    "symbol": "BTCUSDT",
    "target_symbol": "BTCUSDT",
    "target_hedge_ratio": "1",
    "first_trigger_usdt": "5000",
    "rebalance_usdt": "2000",
    "exit_usdt": "1500",
    "max_slippage_bps": 30,
    "min_order_usdt": "10",
    "leverage": 1,
    "enabled": true,
    "dry_run": false
  }'
```

Submit a platform exposure snapshot:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/exposures \
  -H 'Content-Type: application/json' \
  -d '{
    "agent_id": 1,
    "source": "platform",
    "symbol": "BTCUSDT",
    "net_quantity": "0.2",
    "net_notional_usdt": "10000",
    "mark_price": "50000"
  }'
```

Run the ClickHouse/Redis exposure sync immediately:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/exposures/sync
```

Synchronize configured exchange positions immediately:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/positions/sync
```

Refresh and query the exposure monitoring read model:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/hedge-monitor/refresh
curl 'http://127.0.0.1:7892/api/v1/hedge-monitor?agent_id=1'
```

Run one hedge decision:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/hedge/run \
  -H 'Content-Type: application/json' \
  -d '{"config_id": 1}'
```

Exit the current hedge position with a reverse `reduce_only` order:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/hedge/exit \
  -H 'Content-Type: application/json' \
  -d '{"config_id": 1}'
```

Enqueue one hedge decision through Asynq:

```bash
curl -X POST http://127.0.0.1:7892/api/v1/hedge/enqueue \
  -H 'Content-Type: application/json' \
  -d '{"config_id": 1}'
```

Prometheus metrics:

```bash
curl http://127.0.0.1:7892/metrics
```

## gRPC

The gRPC service is defined in `proto/hedging/v1/hedging.proto`.

```text
hedging.v1.HedgingService/UpsertExposure
hedging.v1.HedgingService/UpsertPosition
hedging.v1.HedgingService/RunHedge
hedging.v1.HedgingService/ExitHedge
hedging.v1.HedgingService/EnqueueHedgeRun
```

## Development

```bash
make sqlc
make proto
make tidy
make test
```
