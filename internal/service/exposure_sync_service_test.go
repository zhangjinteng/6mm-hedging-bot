package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/clickhousehist"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

type fakeExposureSyncRepository struct {
	configs []mgmt.HedgeMonitorConfig
	err     error
}

func (repository *fakeExposureSyncRepository) ListHedgeMonitorConfigs(context.Context) ([]mgmt.HedgeMonitorConfig, error) {
	return repository.configs, repository.err
}

func executableMonitorConfig(config mgmt.HedgeConfig) mgmt.HedgeMonitorConfig {
	config.Enabled = true
	config.ExchangeAccountID = config.ID + 100
	config.ExchangeAccount = mgmt.ExchangeAccount{
		ID:      config.ExchangeAccountID,
		AgentID: config.AgentID,
		Status:  mgmt.ExchangeAccountStatusActive,
	}
	return mgmt.HedgeMonitorConfig{Config: config, GlobalEnabled: true}
}

type fakeNetExposureReader struct {
	keys   []clickhousehist.ExposureKey
	result map[clickhousehist.ExposureKey]clickhousehist.NetExposure
	err    error
}

func (reader *fakeNetExposureReader) ListNetExposures(_ context.Context, keys []clickhousehist.ExposureKey) (map[clickhousehist.ExposureKey]clickhousehist.NetExposure, error) {
	reader.keys = append([]clickhousehist.ExposureKey(nil), keys...)
	return reader.result, reader.err
}

type fakeExposureCache struct {
	markPrice MarkPrice
	priceErr  error
	saved     []NetExposureCache
	saveErr   error
}

func (cache *fakeExposureCache) FetchMarkPrice(context.Context, string) (MarkPrice, error) {
	return cache.markPrice, cache.priceErr
}

func (cache *fakeExposureCache) SaveNetExposure(_ context.Context, exposure NetExposureCache) error {
	cache.saved = append(cache.saved, exposure)
	return cache.saveErr
}

type fakeExposureSnapshotWriter struct {
	params []coredb.UpsertExposureSnapshotParams
	err    error
}

func (writer *fakeExposureSnapshotWriter) UpsertExposureSnapshot(_ context.Context, params coredb.UpsertExposureSnapshotParams) (coredb.ExposureSnapshot, error) {
	writer.params = append(writer.params, params)
	return coredb.ExposureSnapshot{}, writer.err
}

type fakeHedgeRunEnqueuer struct {
	inputs []RunInput
	err    error
}

type fakeHedgeMonitorRefresher struct {
	calls  int
	result HedgeMonitorRefreshResult
	err    error
}

func (monitor *fakeHedgeMonitorRefresher) Refresh(context.Context) (HedgeMonitorRefreshResult, error) {
	monitor.calls++
	return monitor.result, monitor.err
}

func (queue *fakeHedgeRunEnqueuer) EnqueueRunHedge(_ context.Context, input RunInput) (*asynq.TaskInfo, error) {
	queue.inputs = append(queue.inputs, input)
	return nil, queue.err
}

func TestExposureSyncWritesSnapshotAndEnqueuesMatchingConfigs(t *testing.T) {
	configs := []mgmt.HedgeMonitorConfig{
		executableMonitorConfig(mgmt.HedgeConfig{ID: 11, AgentID: 1, Source: "platform", Symbol: "bnbusdt"}),
		executableMonitorConfig(mgmt.HedgeConfig{ID: 12, AgentID: 1, Source: "platform", Symbol: "BNBUSDT"}),
	}
	key := clickhousehist.ExposureKey{AgentID: 1, Symbol: "BNBUSDT"}
	reader := &fakeNetExposureReader{result: map[clickhousehist.ExposureKey]clickhousehist.NetExposure{
		key: {
			AgentID:      1,
			Symbol:       "BNBUSDT",
			NetQuantity:  decimal.RequireFromString("2.5"),
			PositionRows: 4,
		},
	}}
	cache := &fakeExposureCache{markPrice: MarkPrice{
		Symbol:     "BNBUSDT",
		Value:      decimal.RequireFromString("690.8"),
		ObservedAt: time.Now().UTC(),
	}}
	writer := &fakeExposureSnapshotWriter{}
	queue := &fakeHedgeRunEnqueuer{}
	service := NewExposureSyncService(&fakeExposureSyncRepository{configs: configs}, reader, cache, writer, queue)
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	result, err := service.SyncEnabledConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.keys) != 1 || reader.keys[0] != key {
		t.Fatalf("unexpected clickhouse keys %+v", reader.keys)
	}
	if len(cache.saved) != 1 || !cache.saved[0].NetQuantity.Equal(decimal.RequireFromString("2.5")) {
		t.Fatalf("unexpected cached exposure %+v", cache.saved)
	}
	if len(writer.params) != 1 {
		t.Fatalf("unexpected snapshot writes %+v", writer.params)
	}
	params := writer.params[0]
	if params.AgentID != 1 || !params.NetNotionalUsdt.Equal(decimal.RequireFromString("1727")) {
		t.Fatalf("unexpected snapshot params %+v", params)
	}
	if len(queue.inputs) != 2 || queue.inputs[0].ConfigID != 11 || queue.inputs[1].ConfigID != 12 {
		t.Fatalf("unexpected hedge tasks %+v", queue.inputs)
	}
	if result.Configs != 2 || result.ExposureGroups != 1 || result.Snapshots != 1 || result.Enqueued != 2 {
		t.Fatalf("unexpected sync result %+v", result)
	}
}

func TestExposureSyncWritesZeroWhenClickHouseHasNoPosition(t *testing.T) {
	config := executableMonitorConfig(mgmt.HedgeConfig{ID: 21, AgentID: 2, Source: "platform", Symbol: "BTCUSDT"})
	reader := &fakeNetExposureReader{result: map[clickhousehist.ExposureKey]clickhousehist.NetExposure{}}
	cache := &fakeExposureCache{markPrice: MarkPrice{Value: decimal.NewFromInt(50000), ObservedAt: time.Now().UTC()}}
	writer := &fakeExposureSnapshotWriter{}
	queue := &fakeHedgeRunEnqueuer{}
	service := NewExposureSyncService(&fakeExposureSyncRepository{configs: []mgmt.HedgeMonitorConfig{config}}, reader, cache, writer, queue)

	result, err := service.SyncEnabledConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.params) != 1 || !writer.params[0].NetQuantity.IsZero() || !writer.params[0].NetNotionalUsdt.IsZero() {
		t.Fatalf("expected zero exposure snapshot, got %+v", writer.params)
	}
	if result.Enqueued != 1 {
		t.Fatalf("expected exit evaluation task, got %+v", result)
	}
}

func TestExposureSyncSkipsSnapshotWhenPriceIsUnavailable(t *testing.T) {
	config := executableMonitorConfig(mgmt.HedgeConfig{ID: 31, AgentID: 3, Source: "platform", Symbol: "ETHUSDT"})
	cache := &fakeExposureCache{priceErr: errors.New("stale price")}
	writer := &fakeExposureSnapshotWriter{}
	queue := &fakeHedgeRunEnqueuer{}
	service := NewExposureSyncService(
		&fakeExposureSyncRepository{configs: []mgmt.HedgeMonitorConfig{config}},
		&fakeNetExposureReader{result: map[clickhousehist.ExposureKey]clickhousehist.NetExposure{}},
		cache,
		writer,
		queue,
	)

	result, err := service.SyncEnabledConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.params) != 0 || len(queue.inputs) != 0 {
		t.Fatal("stale prices must not produce snapshots or hedge tasks")
	}
	if len(result.Failures) != 1 || result.Failures[0].Stage != "read_mark_price" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestExposureSyncCollectsDisabledConfigWithoutEnqueue(t *testing.T) {
	config := executableMonitorConfig(mgmt.HedgeConfig{ID: 41, AgentID: 4, Source: "platform", Symbol: "SOLUSDT"})
	config.Config.Enabled = false
	cache := &fakeExposureCache{markPrice: MarkPrice{Value: decimal.NewFromInt(100), ObservedAt: time.Now().UTC()}}
	writer := &fakeExposureSnapshotWriter{}
	queue := &fakeHedgeRunEnqueuer{}
	service := NewExposureSyncService(
		&fakeExposureSyncRepository{configs: []mgmt.HedgeMonitorConfig{config}},
		&fakeNetExposureReader{result: map[clickhousehist.ExposureKey]clickhousehist.NetExposure{}},
		cache,
		writer,
		queue,
	)

	result, err := service.SyncEnabledConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.params) != 1 {
		t.Fatal("disabled config must still produce an exposure snapshot")
	}
	if len(queue.inputs) != 0 || result.Enqueued != 0 {
		t.Fatal("disabled config must not enqueue a hedge run")
	}
}

func TestExposureSyncRefreshesMonitorWithoutConfigs(t *testing.T) {
	monitor := &fakeHedgeMonitorRefresher{result: HedgeMonitorRefreshResult{Pruned: 2}}
	service := NewExposureSyncService(
		&fakeExposureSyncRepository{},
		&fakeNetExposureReader{},
		&fakeExposureCache{},
		&fakeExposureSnapshotWriter{},
		&fakeHedgeRunEnqueuer{},
	)
	service.SetMonitorRefresher(monitor)

	result, err := service.SyncEnabledConfigs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if monitor.calls != 1 || result.Monitor.Pruned != 2 {
		t.Fatalf("expected monitor refresh without configs, got calls=%d result=%+v", monitor.calls, result)
	}
}
