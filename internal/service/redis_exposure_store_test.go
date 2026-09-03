package service

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestDecodeMarkPriceValues(t *testing.T) {
	price, observedAt, err := decodeMarkPriceValues([]any{"690.786", "1788243790394"})
	if err != nil {
		t.Fatal(err)
	}
	if !price.Equal(decimal.RequireFromString("690.786")) {
		t.Fatalf("unexpected price %s", price)
	}
	if observedAt.UnixMilli() != 1788243790394 {
		t.Fatalf("unexpected observed time %s", observedAt)
	}
}

func TestDecodeMarkPriceValuesRejectsMissingTimestamp(t *testing.T) {
	_, _, err := decodeMarkPriceValues([]any{"690.786", nil})
	if err == nil {
		t.Fatal("expected missing timestamp error")
	}
}

func TestRedisExposureStoreDefaults(t *testing.T) {
	store := NewRedisExposureStore(nil, RedisExposureStoreOptions{})
	if store.marketPriceKeyPrefix != "market_price:6mm:" {
		t.Fatalf("unexpected market price prefix %s", store.marketPriceKeyPrefix)
	}
	if store.exposureKeyPrefix != "hedge:exposure:6mm:" {
		t.Fatalf("unexpected exposure prefix %s", store.exposureKeyPrefix)
	}
	if store.priceMaxAge != 2*time.Minute || store.exposureTTL != 2*time.Minute {
		t.Fatalf("unexpected store timeouts %+v", store)
	}
}
