package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearSymbolConfigEnv(t)
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("GRPC_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("EXCHANGE_ADAPTER", "")
	t.Setenv("EXCHANGE_ENV", "")
	t.Setenv("LOG_OUTPUT", "")
	t.Setenv("LOG_FILE", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("HEDGE_RUN_LOCK_TTL", "")
	t.Setenv("HEDGE_RUN_COOLDOWN", "")
	t.Setenv("SCHEDULER_ENABLED", "")
	t.Setenv("MARKET_SYNC_ENABLED", "")
	t.Setenv("MARKET_SYNC_INTERVAL", "")
	clearExposureSyncEnv(t)
	clearPositionSyncEnv(t)

	cfg := Load()

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("unexpected HTTPAddr %s", cfg.HTTPAddr)
	}
	if cfg.GRPCAddr != ":9090" {
		t.Fatalf("unexpected GRPCAddr %s", cfg.GRPCAddr)
	}
	if cfg.RedisAddr != "" {
		t.Fatalf("unexpected RedisAddr %s", cfg.RedisAddr)
	}
	if cfg.ExchangeAdapter != "simulated" {
		t.Fatalf("unexpected ExchangeAdapter %s", cfg.ExchangeAdapter)
	}
	if cfg.ExchangeEnv != "paper" {
		t.Fatalf("unexpected ExchangeEnv %s", cfg.ExchangeEnv)
	}
	if cfg.LogOutput != "console" || cfg.LogFile != "logs/hedging-bot.log" || cfg.LogLevel != "info" || cfg.LogFormat != "json" {
		t.Fatalf("unexpected logging defaults %+v", cfg)
	}
	if cfg.SchedulerEnabled {
		t.Fatal("scheduler should default to disabled")
	}
	if cfg.HedgeScheduleInterval != time.Minute {
		t.Fatalf("unexpected interval %s", cfg.HedgeScheduleInterval)
	}
	if cfg.HedgeRunLockTTL != 3*time.Minute || cfg.HedgeRunCooldown != 10*time.Second {
		t.Fatalf("unexpected hedge run guard defaults %+v", cfg)
	}
	if cfg.MarketSyncEnabled {
		t.Fatal("market sync should default to disabled")
	}
	if cfg.MarketSyncInterval != 6*time.Hour {
		t.Fatalf("unexpected market sync interval %s", cfg.MarketSyncInterval)
	}
	if cfg.SymbolConfigDatabaseURL != "" {
		t.Fatalf("unexpected symbol config database url %s", cfg.SymbolConfigDatabaseURL)
	}
	if cfg.ExposureSyncEnabled {
		t.Fatal("exposure sync should default to disabled")
	}
	if cfg.ExposureSyncInterval != 30*time.Second || cfg.ExposurePriceMaxAge != 2*time.Minute {
		t.Fatalf("unexpected exposure sync defaults %+v", cfg)
	}
	if cfg.PositionSyncEnabled || cfg.PositionSyncInterval != 10*time.Second || cfg.PositionStaleAfter != time.Minute {
		t.Fatalf("unexpected position sync defaults %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearSymbolConfigEnv(t)
	t.Setenv("HTTP_ADDR", ":18080")
	t.Setenv("GRPC_ADDR", ":19090")
	t.Setenv("SYMBOL_CONFIG_DATABASE_URL", "postgres://freedex_app:secret@127.0.0.1:5432/freedex?sslmode=disable")
	t.Setenv("REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("EXCHANGE_ADAPTER", "ccxt")
	t.Setenv("EXCHANGE_ENV", "live")
	t.Setenv("WORKER_ENABLED", "false")
	t.Setenv("WORKER_CONCURRENCY", "9")
	t.Setenv("LOG_OUTPUT", "both")
	t.Setenv("LOG_FILE", "/tmp/hedging-bot-test.log")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "console")
	t.Setenv("SCHEDULER_ENABLED", "true")
	t.Setenv("HEDGE_SCHEDULE_INTERVAL", "30s")
	t.Setenv("HEDGE_RUN_LOCK_TTL", "3m")
	t.Setenv("HEDGE_RUN_COOLDOWN", "15s")
	t.Setenv("MARKET_SYNC_ENABLED", "true")
	t.Setenv("MARKET_SYNC_INTERVAL", "6h")
	clearExposureSyncEnv(t)
	clearPositionSyncEnv(t)
	t.Setenv("CLICKHOUSE_HISTORY_HOST", "192.168.10.240")
	t.Setenv("CLICKHOUSE_HISTORY_CONNECT_TIMEOUT", "5")
	t.Setenv("CLICKHOUSE_HISTORY_TIMEOUT", "45s")
	t.Setenv("EXPOSURE_SYNC_ENABLED", "true")
	t.Setenv("EXPOSURE_SYNC_INTERVAL", "20s")
	t.Setenv("EXPOSURE_PRICE_MAX_AGE", "90s")
	t.Setenv("EXPOSURE_CACHE_TTL", "3m")
	t.Setenv("POSITION_SYNC_ENABLED", "true")
	t.Setenv("POSITION_SYNC_INTERVAL", "15s")
	t.Setenv("POSITION_STALE_AFTER", "2m")

	cfg := Load()

	if cfg.HTTPAddr != ":18080" || cfg.GRPCAddr != ":19090" {
		t.Fatalf("unexpected addrs %+v", cfg)
	}
	if cfg.RedisAddr != "127.0.0.1:6379" || cfg.RedisDB != 2 {
		t.Fatalf("unexpected redis config %+v", cfg)
	}
	if cfg.SymbolConfigDatabaseURL != "postgres://freedex_app:secret@127.0.0.1:5432/freedex?sslmode=disable" {
		t.Fatalf("unexpected symbol config database url %s", cfg.SymbolConfigDatabaseURL)
	}
	if cfg.ExchangeAdapter != "ccxt" {
		t.Fatalf("unexpected exchange adapter %s", cfg.ExchangeAdapter)
	}
	if cfg.ExchangeEnv != "live" {
		t.Fatalf("unexpected exchange env %s", cfg.ExchangeEnv)
	}
	if cfg.LogOutput != "both" || cfg.LogFile != "/tmp/hedging-bot-test.log" || cfg.LogLevel != "warn" || cfg.LogFormat != "console" {
		t.Fatalf("unexpected logging config %+v", cfg)
	}
	if cfg.WorkerEnabled {
		t.Fatal("worker should be disabled")
	}
	if cfg.WorkerConcurrency != 9 {
		t.Fatalf("unexpected worker concurrency %d", cfg.WorkerConcurrency)
	}
	if !cfg.SchedulerEnabled {
		t.Fatal("scheduler should be enabled")
	}
	if cfg.HedgeScheduleInterval != 30*time.Second {
		t.Fatalf("unexpected interval %s", cfg.HedgeScheduleInterval)
	}
	if cfg.HedgeRunLockTTL != 3*time.Minute || cfg.HedgeRunCooldown != 15*time.Second {
		t.Fatalf("unexpected hedge run guard config %+v", cfg)
	}
	if !cfg.MarketSyncEnabled {
		t.Fatal("market sync should be enabled")
	}
	if cfg.MarketSyncInterval != 6*time.Hour {
		t.Fatalf("unexpected market sync interval %s", cfg.MarketSyncInterval)
	}
	if !cfg.ExposureSyncEnabled || cfg.ExposureSyncInterval != 20*time.Second {
		t.Fatalf("unexpected exposure sync config %+v", cfg)
	}
	if cfg.ClickHouseConnectTimeout != 5*time.Second || cfg.ClickHouseTimeout != 45*time.Second {
		t.Fatalf("unexpected clickhouse timeouts %+v", cfg)
	}
	if cfg.ExposurePriceMaxAge != 90*time.Second || cfg.ExposureCacheTTL != 3*time.Minute {
		t.Fatalf("unexpected exposure cache config %+v", cfg)
	}
	if !cfg.PositionSyncEnabled || cfg.PositionSyncInterval != 15*time.Second || cfg.PositionStaleAfter != 2*time.Minute {
		t.Fatalf("unexpected position sync config %+v", cfg)
	}
}

func clearPositionSyncEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"POSITION_SYNC_ENABLED", "POSITION_SYNC_INTERVAL", "POSITION_STALE_AFTER"} {
		t.Setenv(key, "")
	}
}

func clearExposureSyncEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CLICKHOUSE_HISTORY_HOST",
		"CLICKHOUSE_HISTORY_HTTP_PORT",
		"CLICKHOUSE_HISTORY_DATABASE",
		"CLICKHOUSE_HISTORY_USERNAME",
		"CLICKHOUSE_HISTORY_PASSWORD",
		"CLICKHOUSE_HISTORY_CONNECT_TIMEOUT",
		"CLICKHOUSE_HISTORY_TIMEOUT",
		"EXPOSURE_SYNC_ENABLED",
		"EXPOSURE_SYNC_INTERVAL",
		"EXPOSURE_PRICE_MAX_AGE",
		"EXPOSURE_CACHE_TTL",
		"MARKET_PRICE_KEY_PREFIX",
		"EXPOSURE_CACHE_KEY_PREFIX",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadBuildsSymbolConfigDatabaseURLFromDBEnv(t *testing.T) {
	clearSymbolConfigEnv(t)
	t.Setenv("DB_CONNECTION", "pgsql")
	t.Setenv("DB_HOST", "192.168.10.240")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_DATABASE", "freedex")
	t.Setenv("DB_USERNAME", "freedex_app")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_CONNECT_TIMEOUT", "5")

	cfg := Load()

	expected := "postgres://freedex_app:secret@192.168.10.240:5432/freedex?connect_timeout=5&sslmode=disable"
	if cfg.SymbolConfigDatabaseURL != expected {
		t.Fatalf("unexpected symbol config database url %s", cfg.SymbolConfigDatabaseURL)
	}
}

func clearSymbolConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SYMBOL_CONFIG_DATABASE_URL",
		"DB_CONNECTION",
		"DB_HOST",
		"DB_PORT",
		"DB_DATABASE",
		"DB_USERNAME",
		"DB_PASSWORD",
		"DB_SSLMODE",
		"DB_CONNECT_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
