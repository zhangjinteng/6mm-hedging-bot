package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"google.golang.org/grpc"

	appconfig "github.com/zhangjinteng/6mm-hedging-bot/internal/config"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/credentials"
	appdb "github.com/zhangjinteng/6mm-hedging-bot/internal/db"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/clickhousehist"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/symbolcfg"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/grpcapi"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/httpapi"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/lock"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/logging"
	hedgingv1 "github.com/zhangjinteng/6mm-hedging-bot/internal/pb/hedging/v1"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/redisx"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/scheduler"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/tasks"
)

func main() {
	cfg := appconfig.Load()
	logBundle, err := logging.New(logging.Config{
		Output:  cfg.LogOutput,
		File:    cfg.LogFile,
		Level:   cfg.LogLevel,
		Format:  cfg.LogFormat,
		Service: "6mm-hedging-bot",
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "configure logging: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = logBundle.Close()
	}()
	logger := logBundle.Logger

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sqlDB, err := appdb.OpenSQL(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("open postgres")
	}
	defer sqlDB.Close()

	if cfg.RunMigrations {
		if err := appdb.RunMigrations(rootCtx, sqlDB, cfg.MigrationsDir); err != nil {
			logger.Fatal().Err(err).Msg("run migrations")
		}
	}

	gormDB, err := appdb.OpenGORM(sqlDB)
	if err != nil {
		logger.Fatal().Err(err).Msg("open gorm")
	}

	coreQueries := coredb.New(sqlDB)
	mgmtRepo := mgmt.NewRepository(gormDB)
	var symbolAllowlist service.SymbolAllowlistProvider
	var allowlistProviders []service.SymbolAllowlistProvider
	if cfg.SymbolConfigDatabaseURL != "" {
		symbolConfigDB, err := appdb.OpenSQL(rootCtx, cfg.SymbolConfigDatabaseURL)
		if err != nil {
			logger.Fatal().Err(err).Msg("open symbol config postgres")
		}
		defer symbolConfigDB.Close()
		symbolAllowlist = symbolcfg.NewRepository(symbolConfigDB)
		allowlistProviders = append(allowlistProviders, symbolAllowlist)
	}
	exchangeAdapter, err := exchange.NewAdapterWithOptions(cfg.ExchangeAdapter, exchange.AdapterOptions{
		ExchangeEnv: cfg.ExchangeEnv,
	})
	if err != nil {
		logger.Fatal().Err(err).Str("adapter", cfg.ExchangeAdapter).Str("exchange_env", cfg.ExchangeEnv).Msg("create exchange adapter")
	}
	credentialCodec, err := credentials.NewCodec(cfg.CredentialEncryptionKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("configure credential decryption")
	}
	hedgeService := service.NewHedgeService(coreQueries, mgmtRepo, exchangeAdapter)
	hedgeService.SetCredentialDecoder(credentialCodec.Decrypt)
	hedgeService.SetRunGuardDurations(cfg.HedgeRunLockTTL, cfg.HedgeRunCooldown)
	reconciliationService := service.NewExchangeReconciliationService(coreQueries, mgmtRepo, exchangeAdapter)
	reconciliationService.SetCredentialDecoder(credentialCodec.Decrypt)
	marketSyncService := service.NewMarketSyncService(mgmtRepo, exchangeAdapter, cfg.ExchangeEnv, allowlistProviders...)
	hedgeMonitorService := service.NewHedgeMonitorService(mgmtRepo, coreQueries, cfg.ExposurePriceMaxAge, cfg.PositionStaleAfter)
	positionSyncService := service.NewPositionSyncService(mgmtRepo, coreQueries, exchangeAdapter, hedgeMonitorService)
	positionSyncService.SetCredentialDecoder(credentialCodec.Decrypt)

	redisClient, err := redisx.Open(rootCtx, redisx.Config{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("open redis")
	}
	if redisClient != nil {
		defer redisClient.Close()
		hedgeService.SetLocker(lock.NewRedisLocker(redisClient))
	}

	var enqueuer *tasks.Enqueuer
	var asynqClient *asynq.Client
	var asynqServer *asynq.Server
	if cfg.RedisAddr != "" {
		redisOpt := asynq.RedisClientOpt{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}
		asynqClient = asynq.NewClient(redisOpt)
		defer asynqClient.Close()
		enqueuer = tasks.NewEnqueuer(asynqClient)
		hedgeService.SetOrderReconciliationScheduler(enqueuer)
		closeRequests, err := mgmtRepo.ListActiveHedgeCloseRequests(rootCtx)
		if err != nil {
			logger.Fatal().Err(err).Msg("recover active hedge close requests")
		}
		for _, request := range closeRequests {
			if request.OrderExecutionID == nil {
				if err := mgmtRepo.FailHedgeClose(rootCtx, request.ID, "service restarted before close order submission"); err != nil {
					logger.Error().Err(err).Uint("close_request_id", request.ID).Msg("fail interrupted hedge close request")
				}
				continue
			}
			if err := enqueuer.ScheduleCloseOrderReconciliation(rootCtx, *request.OrderExecutionID); err != nil {
				logger.Error().Err(err).Uint("close_request_id", request.ID).Msg("recover hedge close reconciliation")
			}
		}
		if len(closeRequests) > 0 {
			logger.Info().Int("close_requests", len(closeRequests)).Msg("recovered active hedge close requests")
		}

		if cfg.WorkerEnabled {
			asynqServer = asynq.NewServer(redisOpt, asynq.Config{
				Concurrency: cfg.WorkerConcurrency,
				Logger:      logBundle.AsynqLogger,
				LogLevel:    logBundle.AsynqLogLevel,
				Queues: map[string]int{
					tasks.QueueHedge:          10,
					tasks.QueueReconciliation: 10,
					"default":                 1,
				},
			})
			mux := asynq.NewServeMux()
			tasks.NewHandler(hedgeService, logger).Register(mux)
			tasks.NewReconciliationHandler(hedgeService, reconciliationService, enqueuer, logger).Register(mux)
			go func() {
				logger.Info().Int("concurrency", cfg.WorkerConcurrency).Msg("asynq worker started")
				if err := asynqServer.Run(mux); err != nil {
					select {
					case <-rootCtx.Done():
						logger.Info().Err(err).Msg("asynq worker stopped")
						return
					default:
					}
					logger.Fatal().Err(err).Msg("asynq worker stopped unexpectedly")
				}
			}()
		}

		if cfg.SchedulerEnabled {
			cron := scheduler.New(mgmtRepo, enqueuer, cfg.HedgeScheduleInterval, logger)
			if err := cron.Start(rootCtx, cfg.HedgeScheduleInterval); err != nil {
				logger.Fatal().Err(err).Msg("start hedge scheduler")
			}
		}
	} else {
		logger.Warn().Msg("redis is not configured; async queue, distributed lock and hedge scheduler are disabled")
	}

	if cfg.MarketSyncEnabled {
		cron := scheduler.NewMarketSync(marketSyncService, logger)
		if err := cron.Start(rootCtx, cfg.MarketSyncInterval); err != nil {
			logger.Fatal().Err(err).Msg("start market sync scheduler")
		}
	}

	if cfg.PositionSyncEnabled {
		cron := scheduler.NewPositionSync(positionSyncService, logger)
		if err := cron.Start(rootCtx, cfg.PositionSyncInterval); err != nil {
			logger.Fatal().Err(err).Msg("start exchange position sync scheduler")
		}
	}

	var exposureSyncService *service.ExposureSyncService
	if cfg.ExposureSyncEnabled {
		if redisClient == nil || enqueuer == nil {
			logger.Fatal().Msg("redis and asynq are required when exposure sync is enabled")
		}
		historyClient, err := clickhousehist.NewClient(clickhousehist.Config{
			Host:           cfg.ClickHouseHistoryHost,
			HTTPPort:       cfg.ClickHouseHistoryPort,
			Database:       cfg.ClickHouseHistoryDB,
			Username:       cfg.ClickHouseHistoryUser,
			Password:       cfg.ClickHouseHistoryPass,
			ConnectTimeout: cfg.ClickHouseConnectTimeout,
			Timeout:        cfg.ClickHouseTimeout,
		})
		if err != nil {
			logger.Fatal().Err(err).Msg("configure clickhouse exposure reader")
		}
		exposureCache := service.NewRedisExposureStore(redisClient, service.RedisExposureStoreOptions{
			MarketPriceKeyPrefix: cfg.MarketPriceKeyPrefix,
			ExposureKeyPrefix:    cfg.ExposureCacheKeyPrefix,
			PriceMaxAge:          cfg.ExposurePriceMaxAge,
			ExposureTTL:          cfg.ExposureCacheTTL,
		})
		exposureSyncService = service.NewExposureSyncService(mgmtRepo, historyClient, exposureCache, coreQueries, enqueuer)
		exposureSyncService.SetMonitorRefresher(hedgeMonitorService)
		cron := scheduler.NewExposureSync(exposureSyncService, logger)
		if err := cron.Start(rootCtx, cfg.ExposureSyncInterval); err != nil {
			logger.Fatal().Err(err).Msg("start exposure sync scheduler")
		}
		if !cfg.WorkerEnabled {
			logger.Warn().Msg("exposure sync can enqueue tasks, but the local asynq worker is disabled")
		}
		if cfg.SchedulerEnabled {
			logger.Warn().Msg("both exposure sync and legacy hedge scheduler are enabled; duplicate hedge tasks will be de-duplicated by asynq")
		}
	}

	api := httpapi.NewServer(hedgeService, mgmtRepo, enqueuer, exchangeAdapter)
	api.SetSymbolAllowlistProvider(symbolAllowlist)
	api.SetExposureSyncService(exposureSyncService)
	api.SetPositionSyncService(positionSyncService)
	api.SetHedgeMonitorService(hedgeMonitorService)
	api.SetExchangeReconciliationService(reconciliationService)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api.Handler(),
	}

	go func() {
		logger.Info().Str("addr", cfg.HTTPAddr).Msg("http server started")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server stopped unexpectedly")
		}
	}()

	grpcServer := grpc.NewServer()
	hedgingv1.RegisterHedgingServiceServer(grpcServer, grpcapi.NewServer(hedgeService, enqueuer))
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Fatal().Err(err).Str("addr", cfg.GRPCAddr).Msg("listen grpc")
	}
	go func() {
		logger.Info().Str("addr", cfg.GRPCAddr).Msg("grpc server started")
		if err := grpcServer.Serve(grpcListener); err != nil {
			select {
			case <-rootCtx.Done():
				logger.Info().Err(err).Msg("grpc server stopped")
				return
			default:
			}
			logger.Fatal().Err(err).Msg("grpc server stopped unexpectedly")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()

	logger.Info().Msg("shutdown requested")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn().Err(err).Msg("http shutdown failed")
	}

	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()
	select {
	case <-grpcDone:
	case <-shutdownCtx.Done():
		grpcServer.Stop()
	}

	if asynqServer != nil {
		asynqServer.Shutdown()
	}
}
