package scheduler

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type fakeMarketSyncer struct {
	calls chan struct{}
}

func (syncer *fakeMarketSyncer) SyncActiveAccounts(ctx context.Context) (service.MarketSyncBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return service.MarketSyncBatchResult{}, err
	}
	select {
	case syncer.calls <- struct{}{}:
	default:
	}
	return service.MarketSyncBatchResult{Accounts: 1, SyncedMarkets: 3}, nil
}

func TestMarketSyncSchedulerRunsPeriodically(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer := &fakeMarketSyncer{calls: make(chan struct{}, 1)}
	scheduler := NewMarketSync(syncer, zerolog.New(io.Discard))

	if err := scheduler.Start(ctx, 10*time.Millisecond); err != nil {
		t.Fatalf("start market sync scheduler: %v", err)
	}

	select {
	case <-syncer.calls:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected market sync job to run")
	}
}

func TestMarketSyncSchedulerRejectsMissingSyncer(t *testing.T) {
	scheduler := NewMarketSync(nil, zerolog.New(io.Discard))
	if err := scheduler.Start(context.Background(), time.Hour); err == nil {
		t.Fatal("expected missing syncer error")
	}
}
