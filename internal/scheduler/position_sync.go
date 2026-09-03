package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type PositionSyncer interface {
	Sync(ctx context.Context) (service.PositionSyncBatchResult, error)
}

type PositionSyncScheduler struct {
	cron   *gocron.Scheduler
	syncer PositionSyncer
	logger zerolog.Logger
}

func NewPositionSync(syncer PositionSyncer, logger zerolog.Logger) *PositionSyncScheduler {
	return &PositionSyncScheduler{
		cron:   gocron.NewScheduler(time.UTC),
		syncer: syncer,
		logger: logger,
	}
}

func (s *PositionSyncScheduler) Start(ctx context.Context, interval time.Duration) error {
	if s == nil || s.syncer == nil {
		return errors.New("position syncer is not configured")
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if _, err := s.cron.Every(interval).SingletonMode().Do(func() {
		s.sync(ctx)
	}); err != nil {
		return err
	}
	s.cron.StartAsync()
	s.logger.Info().Dur("interval", interval).Msg("exchange position sync scheduler started")

	go s.sync(ctx)
	go func() {
		<-ctx.Done()
		s.cron.Stop()
		s.logger.Info().Msg("exchange position sync scheduler stopped")
	}()
	return nil
}

func (s *PositionSyncScheduler) sync(ctx context.Context) {
	result, err := s.syncer.Sync(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("sync exchange positions failed")
		return
	}
	event := s.logger.Info()
	if len(result.Failures) > 0 || len(result.Monitor.Failures) > 0 {
		event = s.logger.Warn()
	}
	event.
		Int("accounts", result.Accounts).
		Int("positions", result.Positions).
		Int("monitor_updated", result.Monitor.Updated).
		Interface("failure_items", result.Failures).
		Interface("monitor_failure_items", result.Monitor.Failures).
		Msg("sync exchange positions finished")
}
