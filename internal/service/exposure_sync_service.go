package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/clickhousehist"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

type ExposureSyncRepository interface {
	ListHedgeMonitorConfigs(ctx context.Context) ([]mgmt.HedgeMonitorConfig, error)
}

type NetExposureReader interface {
	ListNetExposures(ctx context.Context, keys []clickhousehist.ExposureKey) (map[clickhousehist.ExposureKey]clickhousehist.NetExposure, error)
}

type ExposureCache interface {
	FetchMarkPrice(ctx context.Context, symbol string) (MarkPrice, error)
	SaveNetExposure(ctx context.Context, exposure NetExposureCache) error
}

type ExposureSnapshotWriter interface {
	UpsertExposureSnapshot(ctx context.Context, arg coredb.UpsertExposureSnapshotParams) (coredb.ExposureSnapshot, error)
}

type HedgeRunEnqueuer interface {
	EnqueueRunHedge(ctx context.Context, input RunInput) (*asynq.TaskInfo, error)
}

type ExposureSyncService struct {
	repo    ExposureSyncRepository
	reader  NetExposureReader
	cache   ExposureCache
	core    ExposureSnapshotWriter
	queue   HedgeRunEnqueuer
	monitor HedgeMonitorRefresher
	now     func() time.Time
	mu      sync.Mutex
}

type ExposureSyncFailure struct {
	AgentID  uint64 `json:"agent_id"`
	Source   string `json:"source"`
	Symbol   string `json:"symbol"`
	ConfigID uint   `json:"config_id,omitempty"`
	Stage    string `json:"stage"`
	Error    string `json:"error"`
}

type ExposureSyncBatchResult struct {
	Configs        int                       `json:"configs"`
	ExposureGroups int                       `json:"exposure_groups"`
	Snapshots      int                       `json:"snapshots"`
	Enqueued       int                       `json:"enqueued"`
	Skipped        int                       `json:"skipped"`
	Failures       []ExposureSyncFailure     `json:"failures,omitempty"`
	Monitor        HedgeMonitorRefreshResult `json:"monitor"`
}

func (s *ExposureSyncService) SetMonitorRefresher(monitor HedgeMonitorRefresher) {
	s.monitor = monitor
}

func NewExposureSyncService(
	repo ExposureSyncRepository,
	reader NetExposureReader,
	cache ExposureCache,
	core ExposureSnapshotWriter,
	queue HedgeRunEnqueuer,
) *ExposureSyncService {
	return &ExposureSyncService{
		repo:   repo,
		reader: reader,
		cache:  cache,
		core:   core,
		queue:  queue,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (s *ExposureSyncService) SyncEnabledConfigs(ctx context.Context) (ExposureSyncBatchResult, error) {
	if err := s.validate(); err != nil {
		return ExposureSyncBatchResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.repo.ListHedgeMonitorConfigs(ctx)
	if err != nil {
		return ExposureSyncBatchResult{}, fmt.Errorf("list hedge configs for exposure sync: %w", err)
	}
	result := ExposureSyncBatchResult{Configs: len(configs)}
	if len(configs) == 0 {
		if s.monitor != nil {
			monitorResult, err := s.monitor.Refresh(ctx)
			if err != nil {
				return result, fmt.Errorf("refresh hedge monitor without configs: %w", err)
			}
			result.Monitor = monitorResult
		}
		return result, nil
	}

	groups, clickHouseKeys, invalidFailures := groupExposureConfigs(configs)
	result.Failures = append(result.Failures, invalidFailures...)
	result.Skipped += len(invalidFailures)
	result.ExposureGroups = len(groups)

	netExposures, err := s.reader.ListNetExposures(ctx, clickHouseKeys)
	if err != nil {
		return result, fmt.Errorf("aggregate clickhouse net exposures: %w", err)
	}

	groupKeys := make([]exposureGroupKey, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Slice(groupKeys, func(i, j int) bool {
		if groupKeys[i].AgentID != groupKeys[j].AgentID {
			return groupKeys[i].AgentID < groupKeys[j].AgentID
		}
		if groupKeys[i].Source != groupKeys[j].Source {
			return groupKeys[i].Source < groupKeys[j].Source
		}
		return groupKeys[i].Symbol < groupKeys[j].Symbol
	})

	for _, key := range groupKeys {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		configsForGroup := groups[key]
		chKey := clickhousehist.ExposureKey{AgentID: key.AgentID, Symbol: key.Symbol}
		netExposure := netExposures[chKey]
		syncedAt := s.now()

		if err := s.cache.SaveNetExposure(ctx, NetExposureCache{
			AgentID:         key.AgentID,
			Source:          key.Source,
			Symbol:          key.Symbol,
			NetQuantity:     netExposure.NetQuantity,
			PositionRows:    netExposure.PositionRows,
			SourceEventTime: netExposure.SourceEventTime,
			SyncedAt:        syncedAt,
		}); err != nil {
			result.Failures = append(result.Failures, newExposureSyncFailure(key, 0, "cache_net_quantity", err))
			result.Skipped += len(configsForGroup)
			continue
		}

		markPrice, err := s.cache.FetchMarkPrice(ctx, key.Symbol)
		if err != nil {
			result.Failures = append(result.Failures, newExposureSyncFailure(key, 0, "read_mark_price", err))
			result.Skipped += len(configsForGroup)
			continue
		}

		_, err = s.core.UpsertExposureSnapshot(ctx, coredb.UpsertExposureSnapshotParams{
			AgentID:         int64(key.AgentID),
			Source:          key.Source,
			Symbol:          key.Symbol,
			NetQuantity:     netExposure.NetQuantity,
			NetNotionalUsdt: netExposure.NetQuantity.Mul(markPrice.Value),
			MarkPrice:       markPrice.Value,
			ObservedAt:      syncedAt,
		})
		if err != nil {
			result.Failures = append(result.Failures, newExposureSyncFailure(key, 0, "upsert_snapshot", err))
			result.Skipped += len(configsForGroup)
			continue
		}
		result.Snapshots++

		for _, monitorConfig := range configsForGroup {
			if !monitorConfig.CanExecute() {
				result.Skipped++
				continue
			}
			config := monitorConfig.Config
			_, err := s.queue.EnqueueRunHedge(ctx, RunInput{ConfigID: config.ID})
			if errors.Is(err, asynq.ErrDuplicateTask) {
				result.Skipped++
				continue
			}
			if err != nil {
				result.Failures = append(result.Failures, newExposureSyncFailure(key, config.ID, "enqueue_hedge", err))
				continue
			}
			result.Enqueued++
		}
	}

	if s.monitor != nil {
		monitorResult, err := s.monitor.Refresh(ctx)
		if err != nil {
			return result, fmt.Errorf("refresh hedge monitor after exposure sync: %w", err)
		}
		result.Monitor = monitorResult
	}

	return result, nil
}

func (s *ExposureSyncService) validate() error {
	if s == nil || s.repo == nil {
		return errors.New("exposure sync repository is not configured")
	}
	if s.reader == nil {
		return errors.New("clickhouse exposure reader is not configured")
	}
	if s.cache == nil {
		return errors.New("redis exposure cache is not configured")
	}
	if s.core == nil {
		return errors.New("exposure snapshot store is not configured")
	}
	if s.queue == nil {
		return errors.New("hedge queue is not configured")
	}
	return nil
}

type exposureGroupKey struct {
	AgentID uint64
	Source  string
	Symbol  string
}

func groupExposureConfigs(configs []mgmt.HedgeMonitorConfig) (
	map[exposureGroupKey][]mgmt.HedgeMonitorConfig,
	[]clickhousehist.ExposureKey,
	[]ExposureSyncFailure,
) {
	groups := make(map[exposureGroupKey][]mgmt.HedgeMonitorConfig)
	clickHouseKeySet := make(map[clickhousehist.ExposureKey]struct{})
	var failures []ExposureSyncFailure

	for _, monitorConfig := range configs {
		config := monitorConfig.Config
		key := exposureGroupKey{
			AgentID: config.AgentID,
			Source:  normalizeExposureSource(config.Source),
			Symbol:  normalizeExposureSymbol(config.Symbol),
		}
		if key.AgentID == 0 || key.AgentID > math.MaxInt64 || key.Symbol == "" {
			failures = append(failures, newExposureSyncFailure(key, config.ID, "validate_config", errors.New("valid agent_id and symbol are required")))
			continue
		}
		groups[key] = append(groups[key], monitorConfig)
		clickHouseKeySet[clickhousehist.ExposureKey{AgentID: key.AgentID, Symbol: key.Symbol}] = struct{}{}
	}

	clickHouseKeys := make([]clickhousehist.ExposureKey, 0, len(clickHouseKeySet))
	for key := range clickHouseKeySet {
		clickHouseKeys = append(clickHouseKeys, key)
	}
	return groups, clickHouseKeys, failures
}

func newExposureSyncFailure(key exposureGroupKey, configID uint, stage string, err error) ExposureSyncFailure {
	return ExposureSyncFailure{
		AgentID:  key.AgentID,
		Source:   key.Source,
		Symbol:   key.Symbol,
		ConfigID: configID,
		Stage:    stage,
		Error:    err.Error(),
	}
}
