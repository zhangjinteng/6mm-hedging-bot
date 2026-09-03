package scheduler

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type fakePositionSyncer struct {
	calls chan struct{}
}

func (syncer *fakePositionSyncer) Sync(ctx context.Context) (service.PositionSyncBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return service.PositionSyncBatchResult{}, err
	}
	select {
	case syncer.calls <- struct{}{}:
	default:
	}
	return service.PositionSyncBatchResult{Accounts: 1, Positions: 2}, nil
}

func TestPositionSyncSchedulerRunsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer := &fakePositionSyncer{calls: make(chan struct{}, 1)}
	scheduler := NewPositionSync(syncer, zerolog.New(io.Discard))
	if err := scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	select {
	case <-syncer.calls:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected immediate position sync")
	}
}

func TestPositionSyncSchedulerRejectsMissingSyncer(t *testing.T) {
	if err := NewPositionSync(nil, zerolog.New(io.Discard)).Start(context.Background(), time.Second); err == nil {
		t.Fatal("expected missing syncer error")
	}
}
