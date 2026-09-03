package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/observability"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/tasks"
)

type Scheduler struct {
	cron     *gocron.Scheduler
	repo     *mgmt.Repository
	enqueuer *tasks.Enqueuer
	logger   zerolog.Logger
}

func New(repo *mgmt.Repository, enqueuer *tasks.Enqueuer, interval time.Duration, logger zerolog.Logger) *Scheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	cron := gocron.NewScheduler(time.UTC)
	return &Scheduler{
		cron:     cron,
		repo:     repo,
		enqueuer: enqueuer,
		logger:   logger,
	}
}

func (s *Scheduler) Start(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	if _, err := s.cron.Every(interval).Do(func() {
		s.enqueueEnabledConfigs(ctx)
	}); err != nil {
		return err
	}
	s.cron.StartAsync()
	s.logger.Info().Dur("interval", interval).Msg("hedge scheduler started")

	go func() {
		<-ctx.Done()
		s.cron.Stop()
		s.logger.Info().Msg("hedge scheduler stopped")
	}()
	return nil
}

func (s *Scheduler) enqueueEnabledConfigs(ctx context.Context) {
	configs, err := s.repo.ListEnabledHedgeConfigs(ctx)
	if err != nil {
		observability.RecordSchedulerEnqueue("list_error")
		s.logger.Error().Err(err).Msg("list enabled hedge configs failed")
		return
	}
	for _, config := range configs {
		_, err := s.enqueuer.EnqueueRunHedge(ctx, service.RunInput{ConfigID: config.ID})
		if err != nil {
			observability.RecordSchedulerEnqueue("enqueue_error")
			s.logger.Error().Err(err).Uint("config_id", config.ID).Str("symbol", config.Symbol).Msg("enqueue scheduled hedge run failed")
			continue
		}
		observability.RecordSchedulerEnqueue("enqueued")
	}
}
