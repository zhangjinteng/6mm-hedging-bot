package config

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                 string
	GRPCAddr                 string
	DatabaseURL              string
	SymbolConfigDatabaseURL  string
	ClickHouseHistoryHost    string
	ClickHouseHistoryPort    string
	ClickHouseHistoryDB      string
	ClickHouseHistoryUser    string
	ClickHouseHistoryPass    string
	ClickHouseConnectTimeout time.Duration
	ClickHouseTimeout        time.Duration
	CredentialEncryptionKey  string
	MigrationsDir            string
	RunMigrations            bool
	ExchangeAdapter          string
	ExchangeEnv              string
	RedisAddr                string
	RedisPassword            string
	RedisDB                  int
	WorkerEnabled            bool
	WorkerConcurrency        int
	LogOutput                string
	LogFile                  string
	LogLevel                 string
	LogFormat                string
	HedgeRunLockTTL          time.Duration
	HedgeRunCooldown         time.Duration
	SchedulerEnabled         bool
	HedgeScheduleInterval    time.Duration
	MarketSyncEnabled        bool
	MarketSyncInterval       time.Duration
	ExposureSyncEnabled      bool
	ExposureSyncInterval     time.Duration
	ExposurePriceMaxAge      time.Duration
	ExposureCacheTTL         time.Duration
	MarketPriceKeyPrefix     string
	ExposureCacheKeyPrefix   string
	PositionSyncEnabled      bool
	PositionSyncInterval     time.Duration
	PositionStaleAfter       time.Duration
	ShutdownTimeout          time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:                 envString("HTTP_ADDR", ":8080"),
		GRPCAddr:                 envString("GRPC_ADDR", ":9090"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		SymbolConfigDatabaseURL:  symbolConfigDatabaseURL(),
		ClickHouseHistoryHost:    os.Getenv("CLICKHOUSE_HISTORY_HOST"),
		ClickHouseHistoryPort:    envString("CLICKHOUSE_HISTORY_HTTP_PORT", "8123"),
		ClickHouseHistoryDB:      envString("CLICKHOUSE_HISTORY_DATABASE", "freedex_history"),
		ClickHouseHistoryUser:    os.Getenv("CLICKHOUSE_HISTORY_USERNAME"),
		ClickHouseHistoryPass:    os.Getenv("CLICKHOUSE_HISTORY_PASSWORD"),
		ClickHouseConnectTimeout: envDurationOrSeconds("CLICKHOUSE_HISTORY_CONNECT_TIMEOUT", 5*time.Second),
		ClickHouseTimeout:        envDurationOrSeconds("CLICKHOUSE_HISTORY_TIMEOUT", 30*time.Second),
		CredentialEncryptionKey:  os.Getenv("HEDGING_CREDENTIAL_KEY"),
		MigrationsDir:            envString("MIGRATIONS_DIR", "internal/db/migrations"),
		RunMigrations:            envBool("RUN_MIGRATIONS", true),
		ExchangeAdapter:          envString("EXCHANGE_ADAPTER", "simulated"),
		ExchangeEnv:              envString("EXCHANGE_ENV", "paper"),
		RedisAddr:                os.Getenv("REDIS_ADDR"),
		RedisPassword:            os.Getenv("REDIS_PASSWORD"),
		RedisDB:                  envInt("REDIS_DB", 0),
		WorkerEnabled:            envBool("WORKER_ENABLED", true),
		WorkerConcurrency:        envInt("WORKER_CONCURRENCY", 5),
		LogOutput:                envString("LOG_OUTPUT", "console"),
		LogFile:                  envString("LOG_FILE", "logs/hedging-bot.log"),
		LogLevel:                 envString("LOG_LEVEL", "info"),
		LogFormat:                envString("LOG_FORMAT", "json"),
		HedgeRunLockTTL:          envDuration("HEDGE_RUN_LOCK_TTL", 3*time.Minute),
		HedgeRunCooldown:         envDuration("HEDGE_RUN_COOLDOWN", 10*time.Second),
		SchedulerEnabled:         envBool("SCHEDULER_ENABLED", false),
		HedgeScheduleInterval:    envDuration("HEDGE_SCHEDULE_INTERVAL", time.Minute),
		MarketSyncEnabled:        envBool("MARKET_SYNC_ENABLED", false),
		MarketSyncInterval:       envDuration("MARKET_SYNC_INTERVAL", 6*time.Hour),
		ExposureSyncEnabled:      envBool("EXPOSURE_SYNC_ENABLED", false),
		ExposureSyncInterval:     envDuration("EXPOSURE_SYNC_INTERVAL", 30*time.Second),
		ExposurePriceMaxAge:      envDuration("EXPOSURE_PRICE_MAX_AGE", 2*time.Minute),
		ExposureCacheTTL:         envDuration("EXPOSURE_CACHE_TTL", 2*time.Minute),
		MarketPriceKeyPrefix:     envString("MARKET_PRICE_KEY_PREFIX", "market_price:6mm:"),
		ExposureCacheKeyPrefix:   envString("EXPOSURE_CACHE_KEY_PREFIX", "hedge:exposure:6mm:"),
		PositionSyncEnabled:      envBool("POSITION_SYNC_ENABLED", false),
		PositionSyncInterval:     envDuration("POSITION_SYNC_INTERVAL", 10*time.Second),
		PositionStaleAfter:       envDuration("POSITION_STALE_AFTER", time.Minute),
		ShutdownTimeout:          envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func symbolConfigDatabaseURL() string {
	if value := strings.TrimSpace(os.Getenv("SYMBOL_CONFIG_DATABASE_URL")); value != "" {
		return value
	}

	connection := strings.ToLower(strings.TrimSpace(os.Getenv("DB_CONNECTION")))
	if connection != "pgsql" && connection != "postgres" && connection != "postgresql" {
		return ""
	}

	host := strings.TrimSpace(os.Getenv("DB_HOST"))
	database := strings.TrimSpace(os.Getenv("DB_DATABASE"))
	username := strings.TrimSpace(os.Getenv("DB_USERNAME"))
	if host == "" || database == "" || username == "" {
		return ""
	}

	port := strings.TrimSpace(envString("DB_PORT", "5432"))
	password := os.Getenv("DB_PASSWORD")

	databaseURL := url.URL{
		Scheme: "postgres",
		User:   url.User(username),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if password != "" {
		databaseURL.User = url.UserPassword(username, password)
	}

	query := databaseURL.Query()
	query.Set("sslmode", envString("DB_SSLMODE", "disable"))
	if timeout := strings.TrimSpace(os.Getenv("DB_CONNECT_TIMEOUT")); timeout != "" {
		query.Set("connect_timeout", timeout)
	}
	databaseURL.RawQuery = query.Encode()
	return databaseURL.String()
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationOrSeconds(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
