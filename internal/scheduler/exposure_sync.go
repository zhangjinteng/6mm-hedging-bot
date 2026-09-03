package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

type ExposureSyncer interface {
	SyncEnabledConfigs(ctx context.Context) (service.ExposureSyncBatchResult, error)
}

type ExposureSyncScheduler struct {
	cron   *gocron.Scheduler
	syncer ExposureSyncer
	logger zerolog.Logger
}

func NewExposureSync(syncer ExposureSyncer, logger zerolog.Logger) *ExposureSyncScheduler {
	return &ExposureSyncScheduler{
		cron:   gocron.NewScheduler(time.UTC),
		syncer: syncer,
		logger: logger,
	}
}

func (s *ExposureSyncScheduler) Start(ctx context.Context, interval time.Duration) error {
	if s == nil || s.syncer == nil {
		return errors.New("exposure syncer is not configured")
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if _, err := s.cron.Every(interval).SingletonMode().Do(func() {
		s.syncEnabledConfigs(ctx)
	}); err != nil {
		return err
	}
	s.cron.StartAsync()
	s.logger.Info().Dur("interval", interval).Msg("exposure sync scheduler started")

	// 启动后立即同步一次，避免必须等待首个间隔才生成敞口快照。
	go s.syncEnabledConfigs(ctx)
	go func() {
		<-ctx.Done()
		s.cron.Stop()
		s.logger.Info().Msg("exposure sync scheduler stopped")
	}()
	return nil
}

func (s *ExposureSyncScheduler) syncEnabledConfigs(ctx context.Context) {
	result, err := s.syncer.SyncEnabledConfigs(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("sync exposure snapshots failed")
		return
	}
	event := s.logger.Info()
	if len(result.Failures) > 0 || len(result.Monitor.Failures) > 0 {
		event = s.logger.Warn()
	}
	event.
		Int("configs", result.Configs).
		Int("exposure_groups", result.ExposureGroups).
		Int("snapshots", result.Snapshots).
		Int("enqueued", result.Enqueued).
		Int("skipped", result.Skipped).
		Interface("failure_items", result.Failures).
		Interface("monitor_failure_items", result.Monitor.Failures).
		Msg("sync exposure snapshots finished")
}
