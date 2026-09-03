package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type MarketSyncer interface {
	SyncActiveAccounts(ctx context.Context) (service.MarketSyncBatchResult, error)
}

type MarketSyncScheduler struct {
	cron   *gocron.Scheduler
	syncer MarketSyncer
	logger zerolog.Logger
}

func NewMarketSync(syncer MarketSyncer, logger zerolog.Logger) *MarketSyncScheduler {
	return &MarketSyncScheduler{
		cron:   gocron.NewScheduler(time.UTC),
		syncer: syncer,
		logger: logger,
	}
}

func (s *MarketSyncScheduler) Start(ctx context.Context, interval time.Duration) error {
	if s == nil || s.syncer == nil {
		return errors.New("market syncer is not configured")
	}
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if _, err := s.cron.Every(interval).SingletonMode().Do(func() {
		s.syncActiveAccounts(ctx)
	}); err != nil {
		return err
	}
	s.cron.StartAsync()
	s.logger.Info().Dur("interval", interval).Msg("market sync scheduler started")

	go func() {
		<-ctx.Done()
		s.cron.Stop()
		s.logger.Info().Msg("market sync scheduler stopped")
	}()
	return nil
}

func (s *MarketSyncScheduler) syncActiveAccounts(ctx context.Context) {
	result, err := s.syncer.SyncActiveAccounts(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("sync exchange markets failed")
		return
	}
	event := s.logger.Info()
	if len(result.Failures) > 0 {
		event = s.logger.Warn()
	}
	event.
		Int("accounts", result.Accounts).
		Int("synced_markets", result.SyncedMarkets).
		Int("failures", len(result.Failures)).
		Interface("failure_items", result.Failures).
		Msg("sync exchange markets finished")
}
