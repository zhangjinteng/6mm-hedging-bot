package scheduler

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type fakeExposureSyncer struct {
	calls chan struct{}
}

func (syncer *fakeExposureSyncer) SyncEnabledConfigs(ctx context.Context) (service.ExposureSyncBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return service.ExposureSyncBatchResult{}, err
	}
	select {
	case syncer.calls <- struct{}{}:
	default:
	}
	return service.ExposureSyncBatchResult{Configs: 1, Snapshots: 1, Enqueued: 1}, nil
}

func TestExposureSyncSchedulerRunsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer := &fakeExposureSyncer{calls: make(chan struct{}, 1)}
	scheduler := NewExposureSync(syncer, zerolog.New(io.Discard))

	if err := scheduler.Start(ctx, time.Hour); err != nil {
		t.Fatalf("start exposure sync scheduler: %v", err)
	}
	select {
	case <-syncer.calls:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected immediate exposure sync")
	}
}

func TestExposureSyncSchedulerRejectsMissingSyncer(t *testing.T) {
	scheduler := NewExposureSync(nil, zerolog.New(io.Discard))
	if err := scheduler.Start(context.Background(), time.Hour); err == nil {
		t.Fatal("expected missing syncer error")
	}
}
