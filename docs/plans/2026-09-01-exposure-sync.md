# ExposureSync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Periodically aggregate current ClickHouse positions, combine them with the existing Redis mark price, persist agent-scoped exposure snapshots, and enqueue the existing hedge runner.

**Architecture:** A dedicated ExposureSync service reads enabled hedge configurations, groups them by `agent_id + source + symbol`, queries `current_position_query`, reads `market_price:6mm:{symbol}` from Redis, and upserts PostgreSQL snapshots. After a successful snapshot refresh it enqueues each matching hedge configuration; `HedgeService.RunOnce` remains the only component that applies open, rebalance, exit, and minimum-order rules.

**Tech Stack:** Go 1.25, ClickHouse HTTP interface, go-redis, PostgreSQL/sqlc, gocron, Asynq.

---

### Task 1: Make exposure snapshots agent-scoped

**Files:**
- Modify: `internal/db/migrations/000001_init.up.sql`
- Create: `internal/db/migrations/000007_add_exposure_snapshot_agent_id.up.sql`
- Create: `internal/db/migrations/000007_add_exposure_snapshot_agent_id.down.sql`
- Modify: `queries/core.sql`
- Generate: `internal/db/coredb/*`
- Modify: `internal/service/hedge_service.go`

1. Add `agent_id` to the snapshot table and unique key.
2. Add `agent_id` to upsert and lookup queries.
3. Regenerate sqlc code.
4. Pass the hedge configuration's agent ID when reading exposure.

### Task 2: Add ClickHouse and Redis readers

**Files:**
- Create: `internal/db/clickhousehist/client.go`
- Create: `internal/db/clickhousehist/client_test.go`
- Create: `internal/service/redis_exposure_store.go`
- Create: `internal/service/redis_exposure_store_test.go`

1. Query net quantity from `current_position_query` using `agent_id + symbol`.
2. Preserve decimal values as strings until parsed with `decimal.Decimal`.
3. Read `mark` and `ts_mark` from `market_price:6mm:{symbol}`.
4. Reject missing, non-positive, or stale prices.
5. Cache the latest net quantity under an agent-scoped Redis key.

### Task 3: Implement ExposureSync orchestration

**Files:**
- Create: `internal/service/exposure_sync_service.go`
- Create: `internal/service/exposure_sync_service_test.go`

1. Load enabled hedge configurations.
2. De-duplicate ClickHouse and Redis work by `agent_id + source + symbol`.
3. Compute `net_notional_usdt = net_quantity * mark_price`.
4. Upsert `exposure_snapshots`.
5. Enqueue each configuration only after its snapshot succeeds.
6. Return per-item failures without aborting unrelated symbols.

### Task 4: Wire scheduling and manual execution

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Create: `internal/scheduler/exposure_sync.go`
- Create: `internal/scheduler/exposure_sync_test.go`
- Modify: `cmd/hedging-bot/main.go`
- Modify: `internal/httpapi/server.go`
- Modify: `.env.example`

1. Add ClickHouse and ExposureSync environment settings.
2. Start ExposureSync independently of the legacy hedge scheduler.
3. Add `POST /api/v1/exposures/sync` for an immediate run.
4. Require Redis and the Asynq enqueuer when ExposureSync is enabled.

### Task 5: Verify

1. Run `gofmt` on changed Go files.
2. Run sqlc generation.
3. Run focused package tests.
4. Run `go test ./...`.
5. Run `go test -tags ccxt ./...`.
