package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/hedge"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/observability"
)

type HedgeService struct {
	core             HedgeStore
	mgmt             *mgmt.Repository
	exchange         exchange.Adapter
	decodeCredential func(string) (string, error)
	calc             hedge.Calculator
	locker           RunLocker
	lockTTL          time.Duration
	runCooldown      time.Duration
	now              func() time.Time
	reconciler       OrderReconciliationScheduler
}

var ErrRunLocked = errors.New("hedge run already locked")

const (
	reasonRecentOrderPending = "recent hedge order is still submitted"
	reasonRunCooldown        = "hedge run cooldown is active"
	reasonPositionSyncWait   = "waiting for position snapshot after recent fill"
)

type HedgeStore interface {
	coredb.Querier
	GetLatestOrderExecutionStateByConfigID(ctx context.Context, configID int64) (coredb.LatestOrderExecutionState, error)
}

type RunLocker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (func(context.Context) error, bool, error)
}

type OrderReconciliationScheduler interface {
	ScheduleTrackedOrderReconciliation(ctx context.Context, executionID int64) error
	ScheduleCloseOrderReconciliation(ctx context.Context, executionID int64) error
}

func NewHedgeService(core HedgeStore, mgmtRepo *mgmt.Repository, adapter exchange.Adapter) *HedgeService {
	return &HedgeService{
		core:             core,
		mgmt:             mgmtRepo,
		exchange:         adapter,
		decodeCredential: func(value string) (string, error) { return value, nil },
		calc:             hedge.NewCalculator(),
		lockTTL:          3 * time.Minute,
		runCooldown:      10 * time.Second,
		now:              func() time.Time { return time.Now().UTC() },
	}
}

func (s *HedgeService) SetCredentialDecoder(decoder func(string) (string, error)) {
	if decoder != nil {
		s.decodeCredential = decoder
	}
}

func (s *HedgeService) SetLocker(locker RunLocker) {
	s.locker = locker
}

func (s *HedgeService) SetOrderReconciliationScheduler(scheduler OrderReconciliationScheduler) {
	s.reconciler = scheduler
}

func (s *HedgeService) SetRunGuardDurations(lockTTL, cooldown time.Duration) {
	if lockTTL > 0 {
		s.lockTTL = lockTTL
	}
	if cooldown >= 0 {
		s.runCooldown = cooldown
	}
}

type UpsertExposureInput struct {
	AgentID         uint64          `json:"agent_id"`
	Source          string          `json:"source"`
	Symbol          string          `json:"symbol"`
	NetQuantity     decimal.Decimal `json:"net_quantity"`
	NetNotionalUSDT decimal.Decimal `json:"net_notional_usdt"`
	MarkPrice       decimal.Decimal `json:"mark_price"`
	ObservedAt      time.Time       `json:"observed_at"`
}

type UpsertPositionInput struct {
	AgentID           uint64          `json:"agent_id"`
	ExchangeAccountID uint            `json:"exchange_account_id"`
	Exchange          string          `json:"exchange"`
	AccountName       string          `json:"account_name"`
	Symbol            string          `json:"symbol"`
	PositionSide      string          `json:"position_side"`
	Quantity          decimal.Decimal `json:"quantity"`
	NotionalUSDT      decimal.Decimal `json:"notional_usdt"`
	EntryPrice        decimal.Decimal `json:"entry_price"`
	MarkPrice         decimal.Decimal `json:"mark_price"`
	ObservedAt        time.Time       `json:"observed_at"`
}

type RunInput struct {
	ConfigID uint   `json:"config_id"`
	Source   string `json:"source"`
	Symbol   string `json:"symbol"`
	Reason   string `json:"reason"`
}

type ExitInput struct {
	ConfigID uint   `json:"config_id"`
	Source   string `json:"source"`
	Symbol   string `json:"symbol"`
}

type RunResult struct {
	Config       mgmt.HedgeConfig              `json:"config"`
	Exposure     coredb.ExposureSnapshot       `json:"exposure"`
	Position     *coredb.HedgePositionSnapshot `json:"position,omitempty"`
	Decision     hedge.Decision                `json:"decision"`
	OrderPlan    *coredb.OrderPlan             `json:"order_plan,omitempty"`
	Execution    *coredb.OrderExecution        `json:"execution,omitempty"`
	CloseRequest *mgmt.HedgeCloseRequest       `json:"close_request,omitempty"`
}

func (s *HedgeService) UpsertExposure(ctx context.Context, input UpsertExposureInput) (coredb.ExposureSnapshot, error) {
	if input.Source == "" {
		input.Source = "platform"
	}
	if input.ObservedAt.IsZero() {
		input.ObservedAt = time.Now().UTC()
	}
	if input.Symbol == "" {
		return coredb.ExposureSnapshot{}, fmt.Errorf("symbol is required")
	}
	if !input.MarkPrice.GreaterThan(decimal.Zero) {
		return coredb.ExposureSnapshot{}, fmt.Errorf("mark price must be greater than zero")
	}
	return s.core.UpsertExposureSnapshot(ctx, coredb.UpsertExposureSnapshotParams{
		AgentID:         int64(input.AgentID),
		Source:          input.Source,
		Symbol:          input.Symbol,
		NetQuantity:     input.NetQuantity,
		NetNotionalUsdt: input.NetNotionalUSDT,
		MarkPrice:       input.MarkPrice,
		ObservedAt:      input.ObservedAt,
	})
}

func (s *HedgeService) UpsertPosition(ctx context.Context, input UpsertPositionInput) (coredb.HedgePositionSnapshot, error) {
	if input.PositionSide == "" {
		input.PositionSide = "NET"
	}
	if input.ObservedAt.IsZero() {
		input.ObservedAt = time.Now().UTC()
	}
	if input.Exchange == "" || input.AccountName == "" || input.Symbol == "" {
		return coredb.HedgePositionSnapshot{}, fmt.Errorf("exchange, account_name and symbol are required")
	}
	if input.AgentID == 0 || input.ExchangeAccountID == 0 {
		return coredb.HedgePositionSnapshot{}, fmt.Errorf("agent_id and exchange_account_id are required")
	}
	if !input.MarkPrice.GreaterThan(decimal.Zero) {
		return coredb.HedgePositionSnapshot{}, fmt.Errorf("mark price must be greater than zero")
	}
	return s.core.UpsertHedgePositionSnapshot(ctx, coredb.UpsertHedgePositionSnapshotParams{
		AgentID:           int64(input.AgentID),
		ExchangeAccountID: int64(input.ExchangeAccountID),
		Exchange:          input.Exchange,
		AccountName:       input.AccountName,
		Symbol:            input.Symbol,
		PositionSide:      input.PositionSide,
		Quantity:          input.Quantity,
		NotionalUsdt:      input.NotionalUSDT,
		EntryPrice:        input.EntryPrice,
		MarkPrice:         input.MarkPrice,
		ObservedAt:        input.ObservedAt,
	})
}

func (s *HedgeService) RunOnce(ctx context.Context, input RunInput) (RunResult, error) {
	start := time.Now()
	action := "unknown"
	status := "error"
	defer func() {
		observability.RecordHedgeRun(action, status, time.Since(start))
	}()

	config, err := s.loadConfig(ctx, input)
	if err != nil {
		return RunResult{}, err
	}
	if config.LifecycleStatus != "" && config.LifecycleStatus != mgmt.HedgeLifecycleActive {
		action = string(hedge.ActionSkip)
		status = "skipped"
		decision := hedge.Decision{
			Action: hedge.ActionSkip, Symbol: targetSymbolForConfig(config),
			Reason: "hedge config lifecycle is " + config.LifecycleStatus, DryRun: config.DryRun,
		}
		return RunResult{Config: config, Decision: decision}, nil
	}

	if s.locker != nil {
		release, ok, err := s.locker.TryLock(ctx, lockKeyForConfig(config), s.lockTTL)
		if err != nil {
			return RunResult{}, err
		}
		if !ok {
			action = "locked"
			status = "skipped"
			return RunResult{}, ErrRunLocked
		}
		defer func() {
			_ = release(context.Background())
		}()
	}

	if input.Reason == "" {
		blockReason, err := s.automaticRunBlockReason(ctx, config)
		if err != nil {
			return RunResult{}, err
		}
		if blockReason != "" {
			action = string(hedge.ActionSkip)
			status = "skipped"
			decision := hedge.Decision{
				Action: hedge.ActionSkip,
				Symbol: targetSymbolForConfig(config),
				Reason: blockReason,
				DryRun: config.DryRun,
			}
			result := RunResult{Config: config, Decision: decision}
			_ = s.writeAudit(ctx, "hedge.guard", "info", config.Symbol, blockReason, result)
			return result, nil
		}
	}

	exposure, err := s.core.GetExposureSnapshot(ctx, coredb.GetExposureSnapshotParams{
		AgentID: int64(config.AgentID),
		Source:  config.Source,
		Symbol:  config.Symbol,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("get exposure snapshot: %w", err)
	}

	position, err := s.loadPosition(ctx, config, exposure)
	if err != nil {
		return RunResult{}, err
	}

	decision, err := s.calc.Calculate(calculationConfigForRun(config, input), toExposure(exposure), toPosition(config, position))
	if err != nil {
		return RunResult{}, err
	}
	applyRunReason(input, &decision)
	action = string(decision.Action)

	result := RunResult{
		Config:   config,
		Exposure: exposure,
		Decision: decision,
	}
	if position != nil {
		result.Position = position
	}

	if decision.Action == hedge.ActionHold || decision.Action == hedge.ActionSkip {
		_ = s.writeAudit(ctx, "hedge.decision", auditSeverity(decision.Action), config.Symbol, decision.Reason, result)
		status = "ok"
		return result, nil
	}

	plan, execution, err := s.createPlanAndExecute(ctx, config, exposure, decision)
	if err != nil {
		return RunResult{}, err
	}
	result.OrderPlan = &plan
	result.Execution = &execution
	s.refreshFilledPosition(ctx, config, position, decision, execution, &result)
	_ = s.writeAudit(ctx, "hedge.order", "info", config.Symbol, decision.Reason, result)
	status = "ok"

	return result, nil
}

func (s *HedgeService) ExitHedge(ctx context.Context, input ExitInput) (RunResult, error) {
	start := time.Now()
	action := string(hedge.ActionExit)
	status := "error"
	defer func() {
		observability.RecordHedgeRun(action, status, time.Since(start))
	}()

	config, err := s.loadConfig(ctx, RunInput{
		ConfigID: input.ConfigID,
		Source:   input.Source,
		Symbol:   input.Symbol,
	})
	if err != nil {
		return RunResult{}, err
	}
	config.TargetSymbol = targetSymbolForConfig(config)

	if s.locker != nil {
		release, ok, err := s.locker.TryLock(ctx, lockKeyForConfig(config), s.lockTTL)
		if err != nil {
			return RunResult{}, err
		}
		if !ok {
			action = "locked"
			status = "skipped"
			return RunResult{}, ErrRunLocked
		}
		defer func() {
			_ = release(context.Background())
		}()
	}

	closeRequest, created, err := s.mgmt.BeginHedgeCloseRequest(
		ctx,
		config.ID,
		fmt.Sprintf("close_%d_%d", config.ID, s.nowUTC().UnixNano()),
	)
	if err != nil {
		return RunResult{}, err
	}
	config.LifecycleStatus = mgmt.HedgeLifecycleClosing
	baseResult := RunResult{
		Config: config,
		Decision: hedge.Decision{
			Action: hedge.ActionExit, Symbol: targetSymbolForConfig(config),
			Reason: hedge.ReasonManualClose, DryRun: config.DryRun,
		},
		CloseRequest: &closeRequest,
	}
	if !created {
		if closeRequest.OrderExecutionID != nil {
			baseResult.Execution = &coredb.OrderExecution{
				ID: *closeRequest.OrderExecutionID, Status: closeRequest.Status,
			}
			if s.reconciler != nil {
				_ = s.reconciler.ScheduleCloseOrderReconciliation(context.Background(), *closeRequest.OrderExecutionID)
			}
		}
		status = "ok"
		return baseResult, nil
	}

	position, err := s.loadExitPosition(ctx, config)
	if err != nil {
		_ = s.mgmt.FailHedgeClose(ctx, closeRequest.ID, err.Error())
		return RunResult{}, err
	}
	if position != nil {
		if err := s.ensureExitMarkPrice(ctx, config, position); err != nil {
			_ = s.mgmt.FailHedgeClose(ctx, closeRequest.ID, err.Error())
			return RunResult{}, err
		}
	}

	exposure := coredb.ExposureSnapshot{
		Source:     config.Source,
		Symbol:     config.Symbol,
		ObservedAt: time.Now().UTC(),
	}
	if position != nil {
		exposure.MarkPrice = position.MarkPrice
		exposure.ObservedAt = position.ObservedAt
		if exposure.ObservedAt.IsZero() {
			exposure.ObservedAt = time.Now().UTC()
		}
	}

	decision, err := s.calc.ClosePosition(toHedgeConfig(config), toPosition(config, position))
	if err != nil {
		return RunResult{}, err
	}
	action = string(decision.Action)

	result := RunResult{
		Config:       config,
		Exposure:     exposure,
		Decision:     decision,
		CloseRequest: &closeRequest,
	}
	if position != nil {
		result.Position = position
	}

	if decision.Action == hedge.ActionHold || decision.Action == hedge.ActionSkip {
		if err := s.mgmt.CompleteHedgeClose(ctx, closeRequest.ID); err != nil {
			return RunResult{}, err
		}
		closeRequest.Status = mgmt.HedgeCloseCompleted
		result.Config.Enabled = false
		result.Config.LifecycleStatus = mgmt.HedgeLifecycleDisabled
		_ = s.writeAudit(ctx, "hedge.exit.decision", auditSeverity(decision.Action), config.Symbol, decision.Reason, result)
		status = "ok"
		return result, nil
	}

	plan, execution, err := s.createPlanAndExecute(ctx, config, exposure, decision)
	if err != nil {
		_ = s.mgmt.FailHedgeClose(ctx, closeRequest.ID, err.Error())
		return RunResult{}, err
	}
	requestStatus := mgmt.HedgeCloseSubmitted
	if execution.Status != exchange.OrderStatusSubmitted {
		requestStatus = mgmt.HedgeCloseVerifying
	}
	if err := s.mgmt.AttachHedgeCloseExecution(ctx, closeRequest.ID, execution.ID, requestStatus); err != nil {
		return RunResult{}, err
	}
	closeRequest.OrderExecutionID = &execution.ID
	closeRequest.Status = requestStatus
	result.OrderPlan = &plan
	result.Execution = &execution
	s.refreshFilledPosition(ctx, config, position, decision, execution, &result)
	if execution.Status == exchange.OrderStatusFilled || execution.Status == exchange.OrderStatusDryRun {
		completed, verifyErr := s.finalizeHedgeClose(ctx, config, &closeRequest)
		if verifyErr != nil {
			_ = s.writeAudit(ctx, "hedge.exit.verify", "warn", config.Symbol, verifyErr.Error(), result)
		}
		if completed {
			result.Config.Enabled = false
			result.Config.LifecycleStatus = mgmt.HedgeLifecycleDisabled
		} else if s.reconciler != nil {
			_ = s.reconciler.ScheduleCloseOrderReconciliation(context.Background(), execution.ID)
		}
	} else if s.reconciler != nil {
		_ = s.reconciler.ScheduleCloseOrderReconciliation(context.Background(), execution.ID)
	}
	_ = s.writeAudit(ctx, "hedge.exit.order", "info", config.Symbol, decision.Reason, result)
	status = "ok"

	return result, nil
}

func (s *HedgeService) finalizeHedgeClose(ctx context.Context, config mgmt.HedgeConfig, request *mgmt.HedgeCloseRequest) (bool, error) {
	if config.DryRun {
		if err := s.mgmt.CompleteHedgeClose(ctx, request.ID); err != nil {
			return false, err
		}
		request.Status = mgmt.HedgeCloseCompleted
		return true, nil
	}
	position, err := s.loadLivePosition(ctx, config)
	if err != nil {
		_ = s.mgmt.UpdateHedgeCloseStatus(ctx, request.ID, mgmt.HedgeCloseVerifying)
		request.Status = mgmt.HedgeCloseVerifying
		return false, err
	}
	if position != nil && !position.Quantity.IsZero() {
		if err := s.mgmt.UpdateHedgeCloseStatus(ctx, request.ID, mgmt.HedgeCloseVerifying); err != nil {
			return false, err
		}
		request.Status = mgmt.HedgeCloseVerifying
		return false, nil
	}
	if err := s.mgmt.CompleteHedgeClose(ctx, request.ID); err != nil {
		return false, err
	}
	request.Status = mgmt.HedgeCloseCompleted
	return true, nil
}

func (s *HedgeService) ListAuditEvents(ctx context.Context, limit int32) ([]coredb.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.core.ListRecentAuditEvents(ctx, limit)
}

func (s *HedgeService) loadConfig(ctx context.Context, input RunInput) (mgmt.HedgeConfig, error) {
	if input.ConfigID > 0 {
		return s.mgmt.GetHedgeConfig(ctx, input.ConfigID)
	}
	source := input.Source
	if source == "" {
		source = "platform"
	}
	if input.Symbol == "" {
		return mgmt.HedgeConfig{}, fmt.Errorf("symbol is required when config_id is empty")
	}
	return s.mgmt.FindEnabledHedgeConfig(ctx, source, input.Symbol)
}

func (s *HedgeService) loadPosition(ctx context.Context, config mgmt.HedgeConfig, exposure coredb.ExposureSnapshot) (*coredb.HedgePositionSnapshot, error) {
	position, err := s.loadStoredPosition(ctx, config)
	if err != nil {
		return nil, err
	}
	if position != nil && position.MarkPrice.IsZero() {
		position.MarkPrice = exposure.MarkPrice
	}
	return position, nil
}

func (s *HedgeService) automaticRunBlockReason(ctx context.Context, config mgmt.HedgeConfig) (string, error) {
	state, err := s.core.GetLatestOrderExecutionStateByConfigID(ctx, int64(config.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get latest order execution state: %w", err)
	}

	status := strings.ToLower(strings.TrimSpace(state.Status))
	if status == exchange.OrderStatusSubmitted && s.exchange != nil {
		state, err = s.reconcileSubmittedOrder(ctx, config, state)
		if err != nil {
			_ = s.writeAudit(ctx, "hedge.order.reconcile", "warn", config.Symbol, err.Error(), map[string]any{
				"config_id":    config.ID,
				"execution_id": state.ExecutionID,
			})
			return reasonRecentOrderPending, nil
		}
		status = strings.ToLower(strings.TrimSpace(state.Status))
	}

	switch status {
	case exchange.OrderStatusSubmitted:
		return reasonRecentOrderPending, nil
	case exchange.OrderStatusFilled:
		if s.runCooldown > 0 && s.nowUTC().Before(state.UpdatedAt.Add(s.runCooldown)) {
			return reasonRunCooldown, nil
		}
		position, err := s.loadStoredPosition(ctx, config)
		if err != nil {
			return "", err
		}
		if position == nil || position.ObservedAt.Before(state.UpdatedAt) {
			return reasonPositionSyncWait, nil
		}
	}

	return "", nil
}

func (s *HedgeService) reconcileSubmittedOrder(
	ctx context.Context,
	config mgmt.HedgeConfig,
	state coredb.LatestOrderExecutionState,
) (coredb.LatestOrderExecutionState, error) {
	accountConfig, err := s.accountConfigFromHedgeConfig(config)
	if err != nil {
		return state, err
	}
	order, err := s.exchange.FetchOrder(ctx, exchange.FetchOrderRequest{
		AccountConfig: accountConfig,
		OrderID:       state.ExchangeOrderID.String,
		ClientOrderID: state.ClientOrderID,
		Symbol:        state.Symbol,
	})
	if err != nil {
		return state, fmt.Errorf("fetch submitted hedge order: %w", err)
	}

	status, errorMessage := persistedExecutionStatus(order.Status)
	exchangeOrderID := state.ExchangeOrderID
	if order.ExchangeOrderID != "" {
		exchangeOrderID = sql.NullString{String: order.ExchangeOrderID, Valid: true}
	}
	execution, err := s.core.UpdateOrderExecutionStatus(ctx, coredb.UpdateOrderExecutionStatusParams{
		ID:              state.ExecutionID,
		ExchangeOrderID: exchangeOrderID,
		Status:          status,
		FilledQuantity:  order.FilledQuantity,
		AvgPrice:        order.AvgPrice,
		FeeUsdt:         order.FeeUSDT,
		RawResponse:     rawJSON(order.Raw),
		ErrorMessage:    errorMessage,
	})
	if err != nil {
		return state, fmt.Errorf("update reconciled order execution: %w", err)
	}
	if _, err := s.core.UpdateOrderPlanStatus(ctx, coredb.UpdateOrderPlanStatusParams{
		ID:     state.OrderPlanID,
		Status: status,
	}); err != nil {
		return state, fmt.Errorf("update reconciled order plan: %w", err)
	}
	state.Status = execution.Status
	state.UpdatedAt = execution.UpdatedAt
	if status == exchange.OrderStatusFilled {
		position, positionErr := s.loadStoredPosition(ctx, config)
		if positionErr != nil {
			_ = s.writeAudit(ctx, "hedge.position.refresh", "warn", config.Symbol, positionErr.Error(), map[string]any{
				"config_id":    config.ID,
				"execution_id": execution.ID,
			})
		} else {
			s.refreshFilledPosition(ctx, config, position, hedge.Decision{
				Side:          hedge.Side(strings.ToUpper(state.Side)),
				OrderQuantity: execution.FilledQuantity,
			}, execution, nil)
		}
	}
	return state, nil
}

type orderExecutionStateByIDStore interface {
	GetOrderExecutionStateByExecutionID(ctx context.Context, executionID int64) (coredb.LatestOrderExecutionState, error)
}

// ReconcileOrderExecution refreshes one submitted execution. It is safe to
// call repeatedly; terminal executions are returned without another API call.
func (s *HedgeService) ReconcileOrderExecution(ctx context.Context, executionID int64) (string, error) {
	store, ok := s.core.(orderExecutionStateByIDStore)
	if !ok {
		return "", errors.New("order execution lookup by id is not supported")
	}
	state, err := store.GetOrderExecutionStateByExecutionID(ctx, executionID)
	if err != nil {
		return "", fmt.Errorf("get order execution state: %w", err)
	}
	status := strings.ToLower(strings.TrimSpace(state.Status))
	if status == exchange.OrderStatusSubmitted {
		config, err := s.mgmt.GetHedgeConfig(ctx, uint(state.ConfigID))
		if err != nil {
			return "", err
		}
		state, err = s.reconcileSubmittedOrder(ctx, config, state)
		if err != nil {
			return exchange.OrderStatusSubmitted, err
		}
		status = strings.ToLower(strings.TrimSpace(state.Status))
	}

	request, err := s.mgmt.GetHedgeCloseRequestByExecutionID(ctx, executionID)
	if errors.Is(err, mgmt.ErrNotFound) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	switch status {
	case exchange.OrderStatusFilled, exchange.OrderStatusDryRun:
		config, err := s.mgmt.GetHedgeConfig(ctx, request.ConfigID)
		if err != nil {
			return mgmt.HedgeCloseVerifying, err
		}
		completed, err := s.finalizeHedgeClose(ctx, config, &request)
		if err != nil {
			return mgmt.HedgeCloseVerifying, err
		}
		if !completed {
			return mgmt.HedgeCloseVerifying, nil
		}
		return exchange.OrderStatusFilled, nil
	case exchange.OrderStatusFailed, exchange.OrderStatusCanceled:
		if err := s.mgmt.FailHedgeClose(ctx, request.ID, "close order ended with status "+status); err != nil {
			return status, err
		}
	}
	return status, nil
}

func (s *HedgeService) FailCloseReconciliation(ctx context.Context, executionID int64, message string) error {
	request, err := s.mgmt.GetHedgeCloseRequestByExecutionID(ctx, executionID)
	if errors.Is(err, mgmt.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if request.Status == mgmt.HedgeCloseCompleted || request.Status == mgmt.HedgeCloseFailed {
		return nil
	}
	return s.mgmt.FailHedgeClose(ctx, request.ID, message)
}

func persistedExecutionStatus(status string) (string, sql.NullString) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case exchange.OrderStatusFilled:
		return exchange.OrderStatusFilled, sql.NullString{}
	case exchange.OrderStatusFailed:
		return exchange.OrderStatusFailed, sql.NullString{String: "exchange order failed", Valid: true}
	case exchange.OrderStatusCanceled:
		return exchange.OrderStatusFailed, sql.NullString{String: "exchange order canceled", Valid: true}
	default:
		return exchange.OrderStatusSubmitted, sql.NullString{}
	}
}

func (s *HedgeService) loadExitPosition(ctx context.Context, config mgmt.HedgeConfig) (*coredb.HedgePositionSnapshot, error) {
	if !config.DryRun && s.exchange != nil {
		position, err := s.loadLivePosition(ctx, config)
		if err != nil {
			return nil, err
		}
		if position != nil {
			return position, nil
		}
	}
	return s.loadStoredPosition(ctx, config)
}

func (s *HedgeService) loadLivePosition(ctx context.Context, config mgmt.HedgeConfig) (*coredb.HedgePositionSnapshot, error) {
	params, err := s.fetchLivePositionSnapshot(ctx, config)
	if err != nil || params == nil {
		return nil, err
	}
	return s.upsertPositionSnapshot(ctx, *params)
}

func (s *HedgeService) fetchLivePositionSnapshot(ctx context.Context, config mgmt.HedgeConfig) (*coredb.UpsertHedgePositionSnapshotParams, error) {
	targetSymbol := targetSymbolForConfig(config)
	accountConfig, err := s.accountConfigFromHedgeConfig(config)
	if err != nil {
		return nil, err
	}
	positions, err := s.exchange.FetchPositions(ctx, exchange.FetchPositionsRequest{
		AccountConfig: accountConfig,
		Symbols:       []string{targetSymbol},
	})
	if err != nil {
		return nil, fmt.Errorf("fetch live hedge position: %w", err)
	}

	for _, position := range positions {
		if !strings.EqualFold(position.Symbol, targetSymbol) {
			continue
		}
		quantity, notional := normalizeExchangePositionValues(position)
		if quantity.IsZero() && notional.IsZero() {
			continue
		}
		observedAt := s.nowUTC()
		positionSide := position.PositionSide
		if positionSide == "" {
			positionSide = "NET"
		}
		exchangeName := position.Exchange
		if exchangeName == "" {
			exchangeName = config.ExchangeAccount.Exchange
		}
		accountName := position.AccountName
		if accountName == "" {
			accountName = config.ExchangeAccount.Name
		}

		params := coredb.UpsertHedgePositionSnapshotParams{
			AgentID:           int64(config.AgentID),
			ExchangeAccountID: int64(config.ExchangeAccountID),
			Exchange:          exchangeName,
			AccountName:       accountName,
			Symbol:            targetSymbol,
			PositionSide:      positionSide,
			Quantity:          quantity,
			NotionalUsdt:      notional,
			EntryPrice:        position.EntryPrice,
			MarkPrice:         position.MarkPrice,
			ObservedAt:        observedAt,
		}
		return &params, nil
	}

	return nil, nil
}

func (s *HedgeService) refreshFilledPosition(
	ctx context.Context,
	config mgmt.HedgeConfig,
	previous *coredb.HedgePositionSnapshot,
	decision hedge.Decision,
	execution coredb.OrderExecution,
	result *RunResult,
) {
	if config.DryRun || strings.ToLower(strings.TrimSpace(execution.Status)) != exchange.OrderStatusFilled {
		return
	}

	params, err := s.fetchLivePositionSnapshot(ctx, config)
	if err == nil && params == nil {
		zero := s.zeroPositionSnapshotParams(config, previous)
		params = &zero
	}
	if err == nil {
		expected := expectedPositionQuantity(previous, decision, execution)
		if !params.Quantity.Equal(expected) {
			err = fmt.Errorf("exchange position has not reflected fill yet: expected quantity %s, got %s", expected, params.Quantity)
		}
	}
	var position *coredb.HedgePositionSnapshot
	if err == nil {
		position, err = s.upsertPositionSnapshot(ctx, *params)
	}
	if err != nil {
		_ = s.writeAudit(ctx, "hedge.position.refresh", "warn", config.Symbol, err.Error(), map[string]any{
			"config_id":    config.ID,
			"execution_id": execution.ID,
		})
		return
	}
	if result != nil {
		result.Position = position
	}
}

func (s *HedgeService) zeroPositionSnapshotParams(
	config mgmt.HedgeConfig,
	previous *coredb.HedgePositionSnapshot,
) coredb.UpsertHedgePositionSnapshotParams {
	markPrice := decimal.Zero
	if previous != nil {
		markPrice = previous.MarkPrice
	}
	return coredb.UpsertHedgePositionSnapshotParams{
		AgentID:           int64(config.AgentID),
		ExchangeAccountID: int64(config.ExchangeAccountID),
		Exchange:          config.ExchangeAccount.Exchange,
		AccountName:       config.ExchangeAccount.Name,
		Symbol:            targetSymbolForConfig(config),
		PositionSide:      "NET",
		Quantity:          decimal.Zero,
		NotionalUsdt:      decimal.Zero,
		EntryPrice:        decimal.Zero,
		MarkPrice:         markPrice,
		ObservedAt:        s.nowUTC(),
	}
}

func (s *HedgeService) upsertPositionSnapshot(ctx context.Context, params coredb.UpsertHedgePositionSnapshotParams) (*coredb.HedgePositionSnapshot, error) {
	snapshot, err := s.core.UpsertHedgePositionSnapshot(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("upsert live hedge position snapshot: %w", err)
	}
	return &snapshot, nil
}

func expectedPositionQuantity(previous *coredb.HedgePositionSnapshot, decision hedge.Decision, execution coredb.OrderExecution) decimal.Decimal {
	quantity := decimal.Zero
	if previous != nil {
		quantity = previous.Quantity
	}
	filled := execution.FilledQuantity.Abs()
	if filled.IsZero() {
		filled = decision.OrderQuantity.Abs()
	}
	if decision.Side == hedge.SideBuy {
		return quantity.Add(filled)
	}
	return quantity.Sub(filled)
}

func (s *HedgeService) loadStoredPosition(ctx context.Context, config mgmt.HedgeConfig) (*coredb.HedgePositionSnapshot, error) {
	position, err := s.core.GetHedgePositionSnapshot(ctx, coredb.GetHedgePositionSnapshotParams{
		AgentID:           int64(config.AgentID),
		ExchangeAccountID: int64(config.ExchangeAccountID),
		Symbol:            targetSymbolForConfig(config),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hedge position snapshot: %w", err)
	}
	return &position, nil
}

func (s *HedgeService) ensureExitMarkPrice(ctx context.Context, config mgmt.HedgeConfig, position *coredb.HedgePositionSnapshot) error {
	if position.MarkPrice.GreaterThan(decimal.Zero) {
		return nil
	}
	if !position.Quantity.IsZero() && !position.NotionalUsdt.IsZero() {
		position.MarkPrice = position.NotionalUsdt.Abs().Div(position.Quantity.Abs())
		return nil
	}
	if s.exchange == nil {
		return fmt.Errorf("exchange adapter is required to fetch exit mark price")
	}

	accountConfig, err := s.accountConfigFromHedgeConfig(config)
	if err != nil {
		return err
	}
	ticker, err := s.exchange.FetchTicker(ctx, exchange.FetchTickerRequest{
		AccountConfig: accountConfig,
		Symbol:        targetSymbolForConfig(config),
	})
	if err != nil {
		return fmt.Errorf("fetch ticker for hedge exit: %w", err)
	}
	price := ticker.Mark
	if !price.GreaterThan(decimal.Zero) {
		price = ticker.Last
	}
	if !price.GreaterThan(decimal.Zero) {
		return fmt.Errorf("ticker mark price must be greater than zero")
	}
	position.MarkPrice = price
	return nil
}

func (s *HedgeService) createPlanAndExecute(ctx context.Context, config mgmt.HedgeConfig, exposure coredb.ExposureSnapshot, decision hedge.Decision) (coredb.OrderPlan, coredb.OrderExecution, error) {
	clientOrderID := makeClientOrderID(config.ID, exposure, decision)
	targetSymbol := targetSymbolForConfig(config)
	status := "planned"
	if config.DryRun {
		status = "dry_run"
	}

	plan, err := s.core.CreateOrderPlan(ctx, coredb.CreateOrderPlanParams{
		IdempotencyKey:    clientOrderID,
		ConfigID:          int64(config.ID),
		AgentID:           int64(config.AgentID),
		ExchangeAccountID: int64(config.ExchangeAccountID),
		Source:            config.Source,
		Exchange:          config.ExchangeAccount.Exchange,
		AccountName:       config.ExchangeAccount.Name,
		Symbol:            targetSymbol,
		Side:              string(decision.Side),
		PositionSide:      "NET",
		OrderType:         "LIMIT",
		Quantity:          decision.OrderQuantity,
		Price:             decision.LimitPrice,
		NotionalUsdt:      decision.AdjustmentUSDT.Abs(),
		ReduceOnly:        decision.ReduceOnly,
		Reason:            decision.Reason,
		Status:            status,
	})
	if err != nil {
		return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("create order plan: %w", err)
	}

	existingExecution, err := s.core.GetOrderExecutionByClientOrderID(ctx, clientOrderID)
	if err == nil {
		if executionErr := existingOrderExecutionError(existingExecution); executionErr != nil {
			return plan, existingExecution, executionErr
		}
		return plan, existingExecution, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("get existing execution: %w", err)
	}

	if config.DryRun {
		execution, err := s.core.CreateOrderExecution(ctx, coredb.CreateOrderExecutionParams{
			OrderPlanID:     plan.ID,
			ClientOrderID:   clientOrderID,
			Status:          "dry_run",
			FilledQuantity:  decimal.Zero,
			AvgPrice:        decimal.Zero,
			FeeUsdt:         decimal.Zero,
			RawResponse:     rawJSON(map[string]any{"dry_run": true}),
			ExchangeOrderID: sql.NullString{},
			ErrorMessage:    sql.NullString{},
		})
		if err != nil {
			return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("create dry-run execution: %w", err)
		}
		return plan, execution, nil
	}

	accountConfig, err := s.accountConfigFromHedgeConfig(config)
	if err != nil {
		return plan, coredb.OrderExecution{}, err
	}

	exchangeResult, err := s.exchange.PlaceOrder(ctx, exchange.PlaceOrderRequest{
		ClientOrderID: clientOrderID,
		Exchange:      accountConfig.Exchange,
		AccountName:   accountConfig.AccountName,
		CCXTID:        accountConfig.CCXTID,
		MarketType:    accountConfig.MarketType,
		Sandbox:       accountConfig.Sandbox,
		DefaultSettle: accountConfig.DefaultSettle,
		AccountType:   accountConfig.AccountType,
		ProductType:   accountConfig.ProductType,
		Category:      accountConfig.Category,
		PositionMode:  accountConfig.PositionMode,
		MarginMode:    accountConfig.MarginMode,
		RecvWindowMS:  accountConfig.RecvWindowMS,
		RateLimitMS:   accountConfig.RateLimitMS,
		APIKey:        accountConfig.APIKey,
		APISecret:     accountConfig.APISecret,
		Passphrase:    accountConfig.Passphrase,
		Symbol:        targetSymbol,
		Side:          string(decision.Side),
		PositionSide:  "NET",
		OrderType:     "LIMIT",
		Quantity:      decision.OrderQuantity,
		Price:         decision.LimitPrice,
		ReduceOnly:    decision.ReduceOnly,
	})
	if err != nil {
		execution, createErr := s.core.CreateOrderExecution(ctx, coredb.CreateOrderExecutionParams{
			OrderPlanID:     plan.ID,
			ClientOrderID:   clientOrderID,
			Status:          "failed",
			FilledQuantity:  decimal.Zero,
			AvgPrice:        decimal.Zero,
			FeeUsdt:         decimal.Zero,
			RawResponse:     rawJSON(map[string]any{}),
			ExchangeOrderID: sql.NullString{},
			ErrorMessage:    sql.NullString{String: err.Error(), Valid: true},
		})
		if createErr != nil {
			return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("place order failed: %w; create failed execution: %v", err, createErr)
		}
		_, _ = s.core.UpdateOrderPlanStatus(ctx, coredb.UpdateOrderPlanStatusParams{ID: plan.ID, Status: "failed"})
		return plan, execution, err
	}

	execution, err := s.core.CreateOrderExecution(ctx, coredb.CreateOrderExecutionParams{
		OrderPlanID:     plan.ID,
		ExchangeOrderID: sql.NullString{String: exchangeResult.ExchangeOrderID, Valid: exchangeResult.ExchangeOrderID != ""},
		ClientOrderID:   clientOrderID,
		Status:          exchangeResult.Status,
		FilledQuantity:  exchangeResult.FilledQuantity,
		AvgPrice:        exchangeResult.AvgPrice,
		FeeUsdt:         exchangeResult.FeeUSDT,
		RawResponse:     rawJSON(exchangeResult.Raw),
		ErrorMessage:    sql.NullString{},
	})
	if err != nil {
		return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("create order execution: %w", err)
	}
	if strings.EqualFold(exchangeResult.Status, exchange.OrderStatusSubmitted) &&
		decision.Reason != hedge.ReasonManualClose && s.reconciler != nil {
		if scheduleErr := s.reconciler.ScheduleTrackedOrderReconciliation(context.Background(), execution.ID); scheduleErr != nil {
			_ = s.writeAudit(ctx, "hedge.order.reconcile.schedule", "warn", config.Symbol, scheduleErr.Error(), map[string]any{
				"config_id": config.ID, "execution_id": execution.ID,
			})
		}
	}

	plan, err = s.core.UpdateOrderPlanStatus(ctx, coredb.UpdateOrderPlanStatusParams{
		ID:     plan.ID,
		Status: exchangeResult.Status,
	})
	if err != nil {
		return coredb.OrderPlan{}, coredb.OrderExecution{}, fmt.Errorf("update order plan status: %w", err)
	}

	return plan, execution, nil
}

func existingOrderExecutionError(execution coredb.OrderExecution) error {
	if !strings.EqualFold(strings.TrimSpace(execution.Status), exchange.OrderStatusFailed) {
		return nil
	}
	if execution.ErrorMessage.Valid && strings.TrimSpace(execution.ErrorMessage.String) != "" {
		return fmt.Errorf("existing order execution failed: %s", execution.ErrorMessage.String)
	}
	return errors.New("existing order execution failed")
}

func (s *HedgeService) writeAudit(ctx context.Context, eventType, severity, symbol, message string, payload any) error {
	_, err := s.core.CreateAuditEvent(ctx, coredb.CreateAuditEventParams{
		EventType: eventType,
		Severity:  severity,
		Symbol:    symbol,
		Message:   message,
		Payload:   rawJSON(payload),
	})
	return err
}

func (s *HedgeService) accountConfigFromHedgeConfig(config mgmt.HedgeConfig) (exchange.AccountConfig, error) {
	return accountConfigFromStoredAccount(config.ExchangeAccount, s.decodeCredential)
}

func toHedgeConfig(config mgmt.HedgeConfig) hedge.Config {
	return hedge.Config{
		ID:               int64(config.ID),
		Source:           config.Source,
		Symbol:           config.Symbol,
		TargetSymbol:     targetSymbolForConfig(config),
		TargetHedgeRatio: config.TargetHedgeRatio,
		FirstTriggerUSDT: config.FirstTriggerUSDT,
		RebalanceUSDT:    config.RebalanceUSDT,
		ExitUSDT:         config.ExitUSDT,
		MaxSlippageBps:   config.MaxSlippageBps,
		MaxNotionalUSDT:  config.MaxNotionalUSDT,
		MinOrderUSDT:     config.MinOrderUSDT,
		Enabled:          config.Enabled,
		DryRun:           config.DryRun,
	}
}

func calculationConfigForRun(config mgmt.HedgeConfig, input RunInput) hedge.Config {
	result := toHedgeConfig(config)
	if input.Reason == hedge.ReasonHedgeRatioAdjustment {
		result.RebalanceUSDT = decimal.Zero
	}
	return result
}

func applyRunReason(input RunInput, decision *hedge.Decision) {
	if input.Reason == hedge.ReasonHedgeRatioAdjustment && decision.Action == hedge.ActionRebalance && decision.Reason == hedge.ReasonRebalance {
		decision.Reason = hedge.ReasonHedgeRatioAdjustment
	}
}

func toExposure(exposure coredb.ExposureSnapshot) hedge.Exposure {
	return hedge.Exposure{
		Source:          exposure.Source,
		Symbol:          exposure.Symbol,
		NetQuantity:     exposure.NetQuantity,
		NetNotionalUSDT: exposure.NetNotionalUsdt,
		MarkPrice:       exposure.MarkPrice,
	}
}

func toPosition(config mgmt.HedgeConfig, position *coredb.HedgePositionSnapshot) hedge.Position {
	if position == nil {
		return hedge.Position{
			Exchange:     config.ExchangeAccount.Exchange,
			AccountName:  config.ExchangeAccount.Name,
			Symbol:       targetSymbolForConfig(config),
			PositionSide: "NET",
		}
	}
	return hedge.Position{
		Exchange:     position.Exchange,
		AccountName:  position.AccountName,
		Symbol:       position.Symbol,
		Quantity:     position.Quantity,
		NotionalUSDT: position.NotionalUsdt,
		MarkPrice:    position.MarkPrice,
		PositionSide: position.PositionSide,
	}
}

func makeClientOrderID(configID uint, exposure coredb.ExposureSnapshot, decision hedge.Decision) string {
	raw := fmt.Sprintf(
		"%d|%d|%s|%s|%s|%s|%s",
		configID,
		exposure.ObservedAt.UTC().UnixNano(),
		decision.Action,
		decision.Symbol,
		decision.TargetNotionalUSDT.String(),
		decision.CurrentNotionalUSDT.String(),
		decision.AdjustmentUSDT.String(),
	)
	sum := sha256.Sum256([]byte(raw))
	// Gate requires custom order text to start with "t-" and limits it to 28
	// characters. Keep one shared format so CCXT does not prepend another "t-"
	// and retries and reconciliation use the same identifier on every exchange.
	return "t-" + hex.EncodeToString(sum[:])[:26]
}

func targetSymbolForConfig(config mgmt.HedgeConfig) string {
	if config.TargetSymbol != "" {
		return config.TargetSymbol
	}
	return config.Symbol
}

func (s *HedgeService) nowUTC() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func rawJSON(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil || len(body) == 0 {
		return json.RawMessage(`{}`)
	}
	return body
}

func auditSeverity(action hedge.Action) string {
	if action == hedge.ActionSkip {
		return "warn"
	}
	return "info"
}

func lockKeyForConfig(config mgmt.HedgeConfig) string {
	return fmt.Sprintf("hedging-bot:run:config:%d", config.ID)
}
