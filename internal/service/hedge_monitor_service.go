package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

const (
	MonitorSwitchEnabled   = "enabled"
	MonitorSwitchGlobalOff = "global_off"
	MonitorSwitchSymbolOff = "symbol_off"

	MonitorHealthOK                 = "ok"
	MonitorHealthAccountUnavailable = "account_unavailable"
	MonitorHealthObserving          = "observing"
	MonitorHealthExecutionFailed    = "execution_failed"

	MonitorActionBalanced          = "balanced"
	MonitorActionOpenRequired      = "open_required"
	MonitorActionRebalanceRequired = "rebalance_required"
	MonitorActionExitRequired      = "exit_required"
)

var monitorStatusLabels = map[string]string{
	MonitorSwitchGlobalOff:          "总开关已关闭",
	MonitorSwitchSymbolOff:          "币种已关闭",
	MonitorHealthAccountUnavailable: "账户不可用",
	MonitorHealthObserving:          "观察中",
	MonitorActionOpenRequired:       "待首次对冲",
	MonitorActionRebalanceRequired:  "待再平衡",
	MonitorActionExitRequired:       "待退出",
	MonitorActionBalanced:           "对冲正常",
	MonitorHealthExecutionFailed:    "执行失败",
}

type HedgeMonitorRepository interface {
	ListHedgeMonitorConfigs(ctx context.Context) ([]mgmt.HedgeMonitorConfig, error)
}

type HedgeMonitorStore interface {
	ListAllExposureSnapshots(ctx context.Context) ([]coredb.ExposureSnapshot, error)
	ListAllHedgePositionSnapshots(ctx context.Context) ([]coredb.HedgePositionSnapshot, error)
	ListLatestOrderExecutionStates(ctx context.Context) ([]coredb.ListLatestOrderExecutionStatesRow, error)
	UpsertHedgeMonitorSnapshot(ctx context.Context, arg coredb.UpsertHedgeMonitorSnapshotParams) (coredb.HedgeMonitorSnapshot, error)
	PruneHedgeMonitorSnapshots(ctx context.Context) (int64, error)
	ListHedgeMonitorSnapshots(ctx context.Context, agentID int64) ([]coredb.HedgeMonitorSnapshot, error)
}

type HedgeMonitorService struct {
	repo          HedgeMonitorRepository
	store         HedgeMonitorStore
	exposureStale time.Duration
	positionStale time.Duration
	now           func() time.Time
	mu            sync.Mutex
}

type HedgeMonitorRefreshResult struct {
	Configs  int      `json:"configs"`
	Updated  int      `json:"updated"`
	Pruned   int64    `json:"pruned"`
	Failures []string `json:"failures,omitempty"`
}

type HedgeMonitorList struct {
	Summary HedgeMonitorSummary        `json:"summary"`
	Items   []HedgeMonitorSnapshotView `json:"items"`
}

type HedgeMonitorSummary struct {
	NetExposureUSDT decimal.Decimal `json:"net_exposure_usdt"`
	TargetHedgeUSDT decimal.Decimal `json:"target_hedge_usdt"`
	ActualHedgeUSDT decimal.Decimal `json:"actual_hedge_usdt"`
	Items           int             `json:"items"`
}

type HedgeMonitorSnapshotView struct {
	ID                 int64           `json:"id"`
	AgentID            int64           `json:"agent_id"`
	ConfigID           int64           `json:"config_id"`
	ExchangeAccountID  int64           `json:"exchange_account_id"`
	Source             string          `json:"source"`
	Symbol             string          `json:"symbol"`
	TargetSymbol       string          `json:"target_symbol"`
	Exchange           string          `json:"exchange"`
	AccountName        string          `json:"account_name"`
	NetQuantity        decimal.Decimal `json:"net_quantity"`
	NetNotionalUSDT    decimal.Decimal `json:"net_notional_usdt"`
	TargetHedgeUSDT    decimal.Decimal `json:"target_hedge_usdt"`
	ActualHedgeUSDT    decimal.Decimal `json:"actual_hedge_usdt"`
	AdjustmentUSDT     decimal.Decimal `json:"adjustment_usdt"`
	SwitchStatus       string          `json:"switch_status"`
	HealthStatus       string          `json:"health_status"`
	ActionStatus       string          `json:"action_status"`
	Status             string          `json:"status"`
	StatusLabel        string          `json:"status_label"`
	StatusReason       string          `json:"status_reason"`
	ExposureObservedAt *time.Time      `json:"exposure_observed_at"`
	PositionObservedAt *time.Time      `json:"position_observed_at"`
	CalculatedAt       time.Time       `json:"calculated_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func NewHedgeMonitorService(
	repo HedgeMonitorRepository,
	store HedgeMonitorStore,
	exposureStale time.Duration,
	positionStale time.Duration,
) *HedgeMonitorService {
	if exposureStale <= 0 {
		exposureStale = 2 * time.Minute
	}
	if positionStale <= 0 {
		positionStale = time.Minute
	}
	return &HedgeMonitorService{
		repo:          repo,
		store:         store,
		exposureStale: exposureStale,
		positionStale: positionStale,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (s *HedgeMonitorService) Refresh(ctx context.Context) (HedgeMonitorRefreshResult, error) {
	if s == nil || s.repo == nil || s.store == nil {
		return HedgeMonitorRefreshResult{}, errors.New("hedge monitor service is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.repo.ListHedgeMonitorConfigs(ctx)
	if err != nil {
		return HedgeMonitorRefreshResult{}, fmt.Errorf("list hedge monitor configs: %w", err)
	}
	exposures, err := s.store.ListAllExposureSnapshots(ctx)
	if err != nil {
		return HedgeMonitorRefreshResult{}, fmt.Errorf("list exposure snapshots for monitoring: %w", err)
	}
	positions, err := s.store.ListAllHedgePositionSnapshots(ctx)
	if err != nil {
		return HedgeMonitorRefreshResult{}, fmt.Errorf("list hedge positions for monitoring: %w", err)
	}
	executions, err := s.store.ListLatestOrderExecutionStates(ctx)
	if err != nil {
		return HedgeMonitorRefreshResult{}, fmt.Errorf("list order executions for monitoring: %w", err)
	}

	exposureByKey := make(map[monitorExposureKey]coredb.ExposureSnapshot, len(exposures))
	for _, exposure := range exposures {
		exposureByKey[monitorExposureKey{
			AgentID: exposure.AgentID,
			Source:  normalizeExposureSource(exposure.Source),
			Symbol:  normalizeExposureSymbol(exposure.Symbol),
		}] = exposure
	}
	positionByKey := make(map[monitorPositionKey]coredb.HedgePositionSnapshot, len(positions))
	for _, position := range positions {
		positionByKey[monitorPositionKey{
			AgentID:   position.AgentID,
			AccountID: position.ExchangeAccountID,
			Symbol:    normalizeMonitorSymbol(position.Symbol),
		}] = position
	}
	executionByConfig := make(map[int64]coredb.ListLatestOrderExecutionStatesRow, len(executions))
	for _, execution := range executions {
		executionByConfig[execution.ConfigID] = execution
	}

	now := s.now()
	result := HedgeMonitorRefreshResult{Configs: len(configs)}
	for _, monitorConfig := range configs {
		config := monitorConfig.Config
		exposure, hasExposure := exposureByKey[monitorExposureKey{
			AgentID: int64(config.AgentID),
			Source:  normalizeExposureSource(config.Source),
			Symbol:  normalizeExposureSymbol(config.Symbol),
		}]
		position, hasPosition := positionByKey[monitorPositionKey{
			AgentID:   int64(config.AgentID),
			AccountID: int64(config.ExchangeAccountID),
			Symbol:    normalizeMonitorSymbol(config.TargetSymbol),
		}]
		execution, hasExecution := executionByConfig[int64(config.ID)]

		params := calculateHedgeMonitorSnapshot(monitorConfig, exposure, hasExposure, position, hasPosition, execution, hasExecution, now, s.exposureStale, s.positionStale)
		if _, err := s.store.UpsertHedgeMonitorSnapshot(ctx, params); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("config_id=%d: %v", config.ID, err))
			continue
		}
		result.Updated++
	}

	pruned, err := s.store.PruneHedgeMonitorSnapshots(ctx)
	if err != nil {
		return result, fmt.Errorf("prune hedge monitor snapshots: %w", err)
	}
	result.Pruned = pruned
	return result, nil
}

func (s *HedgeMonitorService) List(ctx context.Context, agentID uint64) (HedgeMonitorList, error) {
	if s == nil || s.store == nil {
		return HedgeMonitorList{}, errors.New("hedge monitor service is not configured")
	}
	if agentID == 0 {
		return HedgeMonitorList{}, errors.New("agent_id is required")
	}
	items, err := s.store.ListHedgeMonitorSnapshots(ctx, int64(agentID))
	if err != nil {
		return HedgeMonitorList{}, fmt.Errorf("list hedge monitor snapshots: %w", err)
	}
	return buildHedgeMonitorList(items), nil
}

func buildHedgeMonitorList(items []coredb.HedgeMonitorSnapshot) HedgeMonitorList {
	result := HedgeMonitorList{Items: make([]HedgeMonitorSnapshotView, 0, len(items))}
	countedExposures := make(map[monitorExposureKey]struct{}, len(items))
	for _, item := range items {
		result.Items = append(result.Items, hedgeMonitorSnapshotView(item))
		exposureKey := monitorExposureKey{
			AgentID: item.AgentID,
			Source:  normalizeExposureSource(item.Source),
			Symbol:  normalizeExposureSymbol(item.Symbol),
		}
		if _, counted := countedExposures[exposureKey]; !counted {
			result.Summary.NetExposureUSDT = result.Summary.NetExposureUSDT.Add(item.NetNotionalUsdt.Abs())
			countedExposures[exposureKey] = struct{}{}
		}
		result.Summary.TargetHedgeUSDT = result.Summary.TargetHedgeUSDT.Add(item.TargetHedgeUsdt.Abs())
		result.Summary.ActualHedgeUSDT = result.Summary.ActualHedgeUSDT.Add(item.ActualHedgeUsdt.Abs())
	}
	result.Summary.Items = len(result.Items)
	return result
}

func hedgeMonitorSnapshotView(item coredb.HedgeMonitorSnapshot) HedgeMonitorSnapshotView {
	view := HedgeMonitorSnapshotView{
		ID:                item.ID,
		AgentID:           item.AgentID,
		ConfigID:          item.ConfigID,
		ExchangeAccountID: item.ExchangeAccountID,
		Source:            item.Source,
		Symbol:            item.Symbol,
		TargetSymbol:      item.TargetSymbol,
		Exchange:          item.Exchange,
		AccountName:       item.AccountName,
		NetQuantity:       item.NetQuantity,
		NetNotionalUSDT:   item.NetNotionalUsdt,
		TargetHedgeUSDT:   item.TargetHedgeUsdt,
		ActualHedgeUSDT:   item.ActualHedgeUsdt,
		AdjustmentUSDT:    item.AdjustmentUsdt,
		SwitchStatus:      item.SwitchStatus,
		HealthStatus:      item.HealthStatus,
		ActionStatus:      item.ActionStatus,
		Status:            item.Status,
		StatusLabel:       MonitorStatusLabel(item.Status),
		StatusReason:      item.StatusReason,
		CalculatedAt:      item.CalculatedAt,
		UpdatedAt:         item.UpdatedAt,
	}
	if item.ExposureObservedAt.Valid {
		observedAt := item.ExposureObservedAt.Time
		view.ExposureObservedAt = &observedAt
	}
	if item.PositionObservedAt.Valid {
		observedAt := item.PositionObservedAt.Time
		view.PositionObservedAt = &observedAt
	}
	return view
}

func MonitorStatusLabel(status string) string {
	if label, ok := monitorStatusLabels[status]; ok {
		return label
	}
	return status
}

type monitorExposureKey struct {
	AgentID int64
	Source  string
	Symbol  string
}

type monitorPositionKey struct {
	AgentID   int64
	AccountID int64
	Symbol    string
}

func calculateHedgeMonitorSnapshot(
	monitorConfig mgmt.HedgeMonitorConfig,
	exposure coredb.ExposureSnapshot,
	hasExposure bool,
	position coredb.HedgePositionSnapshot,
	hasPosition bool,
	execution coredb.ListLatestOrderExecutionStatesRow,
	hasExecution bool,
	now time.Time,
	exposureStale time.Duration,
	positionStale time.Duration,
) coredb.UpsertHedgeMonitorSnapshotParams {
	config := monitorConfig.Config
	netQuantity := decimal.Zero
	netNotional := decimal.Zero
	target := decimal.Zero
	actual := decimal.Zero
	if hasExposure {
		netQuantity = exposure.NetQuantity
		netNotional = exposure.NetNotionalUsdt
		target = netNotional.Neg().Mul(config.TargetHedgeRatio)
		if config.MaxNotionalUSDT.GreaterThan(decimal.Zero) && target.Abs().GreaterThan(config.MaxNotionalUSDT) {
			target = decimal.NewFromInt(int64(target.Sign())).Mul(config.MaxNotionalUSDT)
		}
		if netNotional.Abs().LessThanOrEqual(config.ExitUSDT) {
			target = decimal.Zero
		}
	}
	if hasPosition {
		actual = signedPositionNotional(position)
	}
	adjustment := target.Sub(actual)

	switchStatus := MonitorSwitchEnabled
	switchReason := ""
	if !monitorConfig.GlobalEnabled {
		switchStatus = MonitorSwitchGlobalOff
		switchReason = "代理商对冲总开关已关闭"
	} else if !config.Enabled {
		switchStatus = MonitorSwitchSymbolOff
		switchReason = "币种对冲配置已关闭"
	}

	healthStatus := MonitorHealthOK
	healthReason := ""
	account := config.ExchangeAccount
	if account.ID == 0 || account.AgentID != config.AgentID || account.Status != mgmt.ExchangeAccountStatusActive {
		healthStatus = MonitorHealthAccountUnavailable
		healthReason = "交易所账户不存在、已停用或代理商归属不一致"
	} else if hasExecution && execution.Status == "failed" {
		healthStatus = MonitorHealthExecutionFailed
		healthReason = strings.TrimSpace(execution.ErrorMessage)
		if healthReason == "" {
			healthReason = "最近一次对冲订单执行失败"
		}
	} else if !hasExposure || now.Sub(exposure.ObservedAt) > exposureStale || !hasPosition || now.Sub(position.ObservedAt) > positionStale {
		healthStatus = MonitorHealthObserving
		healthReason = "正在等待最新敞口或交易所仓位快照"
	}

	actionStatus, actionReason := monitorAction(config, netNotional, actual, adjustment)
	status, reason := mergeMonitorStatus(switchStatus, switchReason, healthStatus, healthReason, actionStatus, actionReason)

	params := coredb.UpsertHedgeMonitorSnapshotParams{
		AgentID:           int64(config.AgentID),
		ConfigID:          int64(config.ID),
		ExchangeAccountID: int64(config.ExchangeAccountID),
		Source:            normalizeExposureSource(config.Source),
		Symbol:            normalizeExposureSymbol(config.Symbol),
		TargetSymbol:      config.TargetSymbol,
		Exchange:          account.Exchange,
		AccountName:       account.Name,
		NetQuantity:       netQuantity,
		NetNotionalUsdt:   netNotional,
		TargetHedgeUsdt:   target,
		ActualHedgeUsdt:   actual,
		AdjustmentUsdt:    adjustment,
		SwitchStatus:      switchStatus,
		HealthStatus:      healthStatus,
		ActionStatus:      actionStatus,
		Status:            status,
		StatusReason:      reason,
		CalculatedAt:      now,
	}
	if hasExposure {
		params.ExposureObservedAt = sql.NullTime{Time: exposure.ObservedAt, Valid: true}
	}
	if hasPosition {
		params.PositionObservedAt = sql.NullTime{Time: position.ObservedAt, Valid: true}
	}
	return params
}

func monitorAction(config mgmt.HedgeConfig, netNotional, actual, adjustment decimal.Decimal) (string, string) {
	if netNotional.Abs().LessThanOrEqual(config.ExitUSDT) {
		if !actual.IsZero() && actual.Abs().GreaterThanOrEqual(config.MinOrderUSDT) {
			return MonitorActionExitRequired, "净敞口已低于退出阈值，但交易所仍有对冲仓位"
		}
		if actual.IsZero() {
			return MonitorActionBalanced, "净敞口低于退出阈值且没有对冲仓位"
		}
		return MonitorActionBalanced, "剩余对冲仓位低于最小下单名义价值"
	}
	if actual.IsZero() {
		if netNotional.Abs().GreaterThanOrEqual(config.FirstTriggerUSDT) && adjustment.Abs().GreaterThanOrEqual(config.MinOrderUSDT) {
			return MonitorActionOpenRequired, "净敞口已达到首次对冲阈值"
		}
		return MonitorActionBalanced, "净敞口尚未达到首次对冲阈值"
	}
	if adjustment.Abs().GreaterThanOrEqual(config.RebalanceUSDT) && adjustment.Abs().GreaterThanOrEqual(config.MinOrderUSDT) {
		return MonitorActionRebalanceRequired, "实际对冲仓位与目标仓位的偏差已达到再平衡阈值"
	}
	return MonitorActionBalanced, "实际对冲仓位处于允许偏差范围内"
}

func mergeMonitorStatus(switchStatus, switchReason, healthStatus, healthReason, actionStatus, actionReason string) (string, string) {
	if switchStatus != MonitorSwitchEnabled {
		return switchStatus, switchReason
	}
	if healthStatus == MonitorHealthAccountUnavailable || healthStatus == MonitorHealthExecutionFailed {
		return healthStatus, healthReason
	}
	if actionStatus != MonitorActionBalanced {
		return actionStatus, actionReason
	}
	if healthStatus == MonitorHealthObserving {
		return healthStatus, healthReason
	}
	return actionStatus, actionReason
}

func signedPositionNotional(position coredb.HedgePositionSnapshot) decimal.Decimal {
	side := strings.ToUpper(strings.TrimSpace(position.PositionSide))
	switch side {
	case "SHORT":
		return position.NotionalUsdt.Abs().Neg()
	case "LONG":
		return position.NotionalUsdt.Abs()
	default:
		if position.Quantity.IsNegative() {
			return position.NotionalUsdt.Abs().Neg()
		}
		if position.Quantity.IsPositive() {
			return position.NotionalUsdt.Abs()
		}
		if !position.NotionalUsdt.IsZero() {
			return position.NotionalUsdt
		}
		return position.NotionalUsdt.Abs()
	}
}

func normalizeMonitorSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
