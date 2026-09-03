package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/observability"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

const (
	TypeReconcileOrder        = "exchange:order:reconcile"
	TypeVerifyPositionSetting = "exchange:position-setting:verify"
	QueueReconciliation       = "reconciliation"
	reconciliationTaskTimeout = 30 * time.Second
)

var reconciliationCheckpoints = [...]time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

var closeReconciliationCheckpoints = [...]time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

type ReconcileOrderPayload struct {
	Attempt       int    `json:"attempt"`
	ExecutionID   int64  `json:"execution_id,omitempty"`
	AccountID     uint   `json:"account_id,omitempty"`
	OrderID       string `json:"order_id,omitempty"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	Symbol        string `json:"symbol,omitempty"`
	CloseRequest  bool   `json:"close_request,omitempty"`
}

type VerifyPositionSettingPayload struct {
	Attempt            int    `json:"attempt"`
	AccountID          uint   `json:"account_id"`
	Symbol             string `json:"symbol"`
	ExpectedLeverage   int    `json:"expected_leverage,omitempty"`
	ExpectedMarginMode string `json:"expected_margin_mode,omitempty"`
}

func (e *Enqueuer) ScheduleTrackedOrderReconciliation(ctx context.Context, executionID int64) error {
	if executionID <= 0 {
		return errors.New("execution id is required")
	}
	return e.enqueueOrderReconciliation(ctx, ReconcileOrderPayload{ExecutionID: executionID})
}

func (e *Enqueuer) ScheduleCloseOrderReconciliation(ctx context.Context, executionID int64) error {
	if executionID <= 0 {
		return errors.New("execution id is required")
	}
	return e.enqueueOrderReconciliation(ctx, ReconcileOrderPayload{ExecutionID: executionID, CloseRequest: true})
}

func (e *Enqueuer) ScheduleExternalOrderReconciliation(ctx context.Context, input service.ExternalOrderReconciliationInput) error {
	if input.AccountID == 0 {
		return errors.New("exchange account id is required")
	}
	return e.enqueueOrderReconciliation(ctx, ReconcileOrderPayload{
		AccountID: input.AccountID, OrderID: input.OrderID, ClientOrderID: input.ClientOrderID, Symbol: input.Symbol,
	})
}

func (e *Enqueuer) ScheduleLeverageVerification(ctx context.Context, accountID uint, symbol string, leverage int) error {
	return e.enqueuePositionSettingVerification(ctx, VerifyPositionSettingPayload{
		AccountID: accountID, Symbol: symbol, ExpectedLeverage: leverage,
	})
}

func (e *Enqueuer) ScheduleMarginModeVerification(ctx context.Context, accountID uint, symbol, marginMode string) error {
	return e.enqueuePositionSettingVerification(ctx, VerifyPositionSettingPayload{
		AccountID: accountID, Symbol: symbol, ExpectedMarginMode: marginMode,
	})
}

func (e *Enqueuer) enqueueOrderReconciliation(ctx context.Context, payload ReconcileOrderPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal order reconciliation payload: %w", err)
	}
	return e.enqueueReconciliationTask(ctx, TypeReconcileOrder, body, payload.Attempt, payload.CloseRequest)
}

func (e *Enqueuer) enqueuePositionSettingVerification(ctx context.Context, payload VerifyPositionSettingPayload) error {
	if payload.AccountID == 0 || strings.TrimSpace(payload.Symbol) == "" {
		return errors.New("exchange account id and symbol are required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal position setting verification payload: %w", err)
	}
	return e.enqueueReconciliationTask(ctx, TypeVerifyPositionSetting, body, payload.Attempt, false)
}

func (e *Enqueuer) enqueueReconciliationTask(ctx context.Context, taskType string, payload []byte, attempt int, closeRequest bool) error {
	if e == nil || e.client == nil {
		return errors.New("async queue is not configured")
	}
	delay, ok := reconciliationDelay(attempt, closeRequest)
	if !ok {
		return nil
	}
	_, err := e.client.EnqueueContext(
		ctx,
		asynq.NewTask(taskType, payload),
		asynq.Queue(QueueReconciliation),
		asynq.ProcessIn(delay),
		asynq.MaxRetry(0),
		asynq.Timeout(reconciliationTaskTimeout),
		asynq.Unique(30*time.Second),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) {
			observability.RecordAsyncTask(taskType, "duplicate")
			return nil
		}
		observability.RecordAsyncTask(taskType, "enqueue_error")
		return fmt.Errorf("enqueue reconciliation task: %w", err)
	}
	observability.RecordAsyncTask(taskType, "enqueued")
	return nil
}

func reconciliationDelay(attempt int, closeRequest bool) (time.Duration, bool) {
	checkpoints := reconciliationCheckpoints[:]
	if closeRequest {
		checkpoints = closeReconciliationCheckpoints[:]
	}
	if attempt < 0 || attempt >= len(checkpoints) {
		return 0, false
	}
	if attempt == 0 {
		return checkpoints[0], true
	}
	return checkpoints[attempt] - checkpoints[attempt-1], true
}

type ReconciliationHandler struct {
	hedge    *service.HedgeService
	exchange *service.ExchangeReconciliationService
	enqueuer *Enqueuer
	logger   zerolog.Logger
}

func NewReconciliationHandler(hedgeService *service.HedgeService, exchangeService *service.ExchangeReconciliationService, enqueuer *Enqueuer, logger zerolog.Logger) *ReconciliationHandler {
	return &ReconciliationHandler{hedge: hedgeService, exchange: exchangeService, enqueuer: enqueuer, logger: logger}
}

func (h *ReconciliationHandler) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeReconcileOrder, h.handleOrder)
	mux.HandleFunc(TypeVerifyPositionSetting, h.handlePositionSetting)
}

func (h *ReconciliationHandler) handleOrder(ctx context.Context, task *asynq.Task) error {
	var payload ReconcileOrderPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		observability.RecordAsyncTask(TypeReconcileOrder, "bad_payload")
		return fmt.Errorf("unmarshal order reconciliation payload: %w: %w", err, asynq.SkipRetry)
	}

	var status string
	var err error
	if payload.ExecutionID > 0 {
		status, err = h.hedge.ReconcileOrderExecution(ctx, payload.ExecutionID)
	} else {
		status, err = h.exchange.ReconcileExternalOrder(ctx, service.ExternalOrderReconciliationInput{
			AccountID: payload.AccountID, OrderID: payload.OrderID, ClientOrderID: payload.ClientOrderID, Symbol: payload.Symbol,
		})
	}
	if err != nil {
		h.logger.Warn().Err(err).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("order reconciliation attempt failed")
		return h.scheduleNextOrder(ctx, payload)
	}
	if isFinalOrderStatus(status) {
		observability.RecordAsyncTask(TypeReconcileOrder, "done")
		h.logger.Info().Str("status", status).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("order reconciliation completed")
		return nil
	}
	h.logger.Info().Str("status", status).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("order reconciliation remains pending")
	return h.scheduleNextOrder(ctx, payload)
}

func (h *ReconciliationHandler) scheduleNextOrder(ctx context.Context, payload ReconcileOrderPayload) error {
	payload.Attempt++
	if _, ok := reconciliationDelay(payload.Attempt, payload.CloseRequest); !ok {
		observability.RecordAsyncTask(TypeReconcileOrder, "exhausted")
		if payload.ExecutionID > 0 {
			_ = h.hedge.FailCloseReconciliation(ctx, payload.ExecutionID, "close reconciliation exhausted after 60 seconds")
		}
		elapsed := reconciliationCheckpoints[len(reconciliationCheckpoints)-1]
		if payload.CloseRequest {
			elapsed = closeReconciliationCheckpoints[len(closeReconciliationCheckpoints)-1]
		}
		h.logger.Warn().Dur("elapsed", elapsed).Interface("payload", payload).Msg("order reconciliation exhausted")
		return nil
	}
	return h.enqueuer.enqueueOrderReconciliation(ctx, payload)
}

func (h *ReconciliationHandler) handlePositionSetting(ctx context.Context, task *asynq.Task) error {
	var payload VerifyPositionSettingPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		observability.RecordAsyncTask(TypeVerifyPositionSetting, "bad_payload")
		return fmt.Errorf("unmarshal position setting verification payload: %w: %w", err, asynq.SkipRetry)
	}
	matched, observed, err := h.exchange.VerifyPositionSetting(ctx, service.PositionSettingVerificationInput{
		AccountID: payload.AccountID, Symbol: payload.Symbol,
		ExpectedLeverage: payload.ExpectedLeverage, ExpectedMarginMode: payload.ExpectedMarginMode,
	})
	if err != nil {
		h.logger.Warn().Err(err).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("position setting verification attempt failed")
		return h.scheduleNextPositionSetting(ctx, payload)
	}
	if matched {
		observability.RecordAsyncTask(TypeVerifyPositionSetting, "done")
		h.logger.Info().Str("observed", observed).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("position setting verification completed")
		return nil
	}
	h.logger.Info().Str("observed", observed).Int("attempt", payload.Attempt+1).Interface("payload", payload).Msg("position setting has not propagated")
	return h.scheduleNextPositionSetting(ctx, payload)
}

func (h *ReconciliationHandler) scheduleNextPositionSetting(ctx context.Context, payload VerifyPositionSettingPayload) error {
	payload.Attempt++
	if _, ok := reconciliationDelay(payload.Attempt, false); !ok {
		observability.RecordAsyncTask(TypeVerifyPositionSetting, "exhausted")
		h.logger.Warn().Interface("payload", payload).Msg("position setting verification exhausted after 10 seconds")
		return nil
	}
	return h.enqueuer.enqueuePositionSettingVerification(ctx, payload)
}

func isFinalOrderStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case exchange.OrderStatusFilled, exchange.OrderStatusCanceled, exchange.OrderStatusFailed:
		return true
	default:
		return false
	}
}
