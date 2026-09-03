//go:build ccxt

package exchange

import (
	"errors"
	"testing"

	sixmmccxt "github.com/zhangjinteng/6mm-ccxt"
)

func TestCCXTAdapterRegistered(t *testing.T) {
	adapter, err := NewAdapter("ccxt")
	if err != nil {
		t.Fatalf("expected ccxt adapter to be registered: %v", err)
	}
	if _, ok := adapter.(CCXTAdapter); !ok {
		t.Fatalf("expected CCXTAdapter, got %T", adapter)
	}
}

func TestNewCCXTAdapterRejectsInvalidExchangeEnv(t *testing.T) {
	if _, err := NewCCXTAdapter(AdapterOptions{ExchangeEnv: "demo"}); err == nil {
		t.Fatal("expected invalid exchange env to fail")
	}
}

func TestFromSixErrorMapsKnownSentinels(t *testing.T) {
	if err := fromSixError(sixmmccxt.ErrOrderNotFound); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	if err := fromSixError(sixmmccxt.ErrOrderAlreadyFinal); !errors.Is(err, ErrOrderAlreadyFinal) {
		t.Fatalf("expected ErrOrderAlreadyFinal, got %v", err)
	}
}
