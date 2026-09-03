package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

type MarkPrice struct {
	Symbol     string
	Value      decimal.Decimal
	ObservedAt time.Time
}

type NetExposureCache struct {
	AgentID         uint64
	Source          string
	Symbol          string
	NetQuantity     decimal.Decimal
	PositionRows    int64
	SourceEventTime time.Time
	SyncedAt        time.Time
}

type RedisExposureStoreOptions struct {
	MarketPriceKeyPrefix string
	ExposureKeyPrefix    string
	PriceMaxAge          time.Duration
	ExposureTTL          time.Duration
}

type RedisExposureStore struct {
	client               redis.Cmdable
	marketPriceKeyPrefix string
	exposureKeyPrefix    string
	priceMaxAge          time.Duration
	exposureTTL          time.Duration
}

func NewRedisExposureStore(client redis.Cmdable, options RedisExposureStoreOptions) *RedisExposureStore {
	if options.MarketPriceKeyPrefix == "" {
		options.MarketPriceKeyPrefix = "market_price:6mm:"
	}
	if options.ExposureKeyPrefix == "" {
		options.ExposureKeyPrefix = "hedge:exposure:6mm:"
	}
	if options.PriceMaxAge <= 0 {
		options.PriceMaxAge = 2 * time.Minute
	}
	if options.ExposureTTL <= 0 {
		options.ExposureTTL = 2 * time.Minute
	}
	return &RedisExposureStore{
		client:               client,
		marketPriceKeyPrefix: options.MarketPriceKeyPrefix,
		exposureKeyPrefix:    options.ExposureKeyPrefix,
		priceMaxAge:          options.PriceMaxAge,
		exposureTTL:          options.ExposureTTL,
	}
}

func (s *RedisExposureStore) FetchMarkPrice(ctx context.Context, symbol string) (MarkPrice, error) {
	if s == nil || s.client == nil {
		return MarkPrice{}, errors.New("redis exposure store is not configured")
	}
	symbol = normalizeExposureSymbol(symbol)
	if symbol == "" {
		return MarkPrice{}, errors.New("symbol is required")
	}

	values, err := s.client.HMGet(ctx, s.marketPriceKeyPrefix+symbol, "mark", "ts_mark").Result()
	if err != nil {
		return MarkPrice{}, fmt.Errorf("read redis mark price for %s: %w", symbol, err)
	}
	markPrice, observedAt, err := decodeMarkPriceValues(values)
	if err != nil {
		return MarkPrice{}, fmt.Errorf("read redis mark price for %s: %w", symbol, err)
	}
	if observedAt.After(time.Now().Add(30 * time.Second)) {
		return MarkPrice{}, fmt.Errorf("redis mark price for %s has a future timestamp: %s", symbol, observedAt.Format(time.RFC3339Nano))
	}
	if age := time.Since(observedAt); age > s.priceMaxAge {
		return MarkPrice{}, fmt.Errorf("redis mark price for %s is stale: age=%s max_age=%s", symbol, age.Round(time.Second), s.priceMaxAge)
	}

	return MarkPrice{Symbol: symbol, Value: markPrice, ObservedAt: observedAt}, nil
}

func (s *RedisExposureStore) SaveNetExposure(ctx context.Context, exposure NetExposureCache) error {
	if s == nil || s.client == nil {
		return errors.New("redis exposure store is not configured")
	}
	exposure.Source = normalizeExposureSource(exposure.Source)
	exposure.Symbol = normalizeExposureSymbol(exposure.Symbol)
	if exposure.AgentID == 0 || exposure.Symbol == "" {
		return errors.New("agent_id and symbol are required")
	}
	if exposure.SyncedAt.IsZero() {
		exposure.SyncedAt = time.Now().UTC()
	}

	values := map[string]any{
		"agent_id":             exposure.AgentID,
		"source":               exposure.Source,
		"symbol":               exposure.Symbol,
		"net_quantity":         exposure.NetQuantity.String(),
		"position_rows":        exposure.PositionRows,
		"source_event_time_ms": unixMilliOrZero(exposure.SourceEventTime),
		"synced_at_ms":         exposure.SyncedAt.UnixMilli(),
	}
	key := fmt.Sprintf("%s%d:%s:%s", s.exposureKeyPrefix, exposure.AgentID, exposure.Source, exposure.Symbol)
	if err := s.client.HSet(ctx, key, values).Err(); err != nil {
		return fmt.Errorf("write redis net exposure for agent=%d symbol=%s: %w", exposure.AgentID, exposure.Symbol, err)
	}
	if s.exposureTTL > 0 {
		if err := s.client.Expire(ctx, key, s.exposureTTL).Err(); err != nil {
			return fmt.Errorf("expire redis net exposure for agent=%d symbol=%s: %w", exposure.AgentID, exposure.Symbol, err)
		}
	}
	return nil
}

func decodeMarkPriceValues(values []any) (decimal.Decimal, time.Time, error) {
	if len(values) < 2 || values[0] == nil {
		return decimal.Zero, time.Time{}, errors.New("mark field is missing")
	}
	markPrice, err := decimal.NewFromString(strings.TrimSpace(fmt.Sprint(values[0])))
	if err != nil || !markPrice.GreaterThan(decimal.Zero) {
		return decimal.Zero, time.Time{}, fmt.Errorf("invalid mark field %q", values[0])
	}
	if values[1] == nil {
		return decimal.Zero, time.Time{}, errors.New("ts_mark field is missing")
	}
	timestampMS, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(values[1])), 10, 64)
	if err != nil || timestampMS <= 0 {
		return decimal.Zero, time.Time{}, fmt.Errorf("invalid ts_mark field %q", values[1])
	}
	return markPrice, time.UnixMilli(timestampMS).UTC(), nil
}

func normalizeExposureSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "platform"
	}
	return strings.ToLower(source)
}

func normalizeExposureSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func unixMilliOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}
