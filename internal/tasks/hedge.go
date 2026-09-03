package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/observability"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
)

const (
	TypeRunHedge = "hedge:run"
	QueueHedge   = "hedge"
)

type RunHedgePayload struct {
	ConfigID uint   `json:"config_id,omitempty"`
	Source   string `json:"source,omitempty"`
	Symbol   string `json:"symbol,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Enqueuer struct {
	client *asynq.Client
}

func NewEnqueuer(client *asynq.Client) *Enqueuer {
	return &Enqueuer{client: client}
}

func (e *Enqueuer) EnqueueRunHedge(ctx context.Context, input service.RunInput) (*asynq.TaskInfo, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("async queue is not configured")
	}
	payload, err := json.Marshal(RunHedgePayload{
		ConfigID: input.ConfigID,
		Source:   input.Source,
		Symbol:   input.Symbol,
		Reason:   input.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal hedge task payload: %w", err)
	}
	task := asynq.NewTask(TypeRunHedge, payload)
	info, err := e.client.EnqueueContext(
		ctx,
		task,
		asynq.Queue(QueueHedge),
		asynq.MaxRetry(3),
		asynq.Timeout(2*time.Minute),
		asynq.Unique(30*time.Second),
	)
	if err != nil {
		observability.RecordAsyncTask(TypeRunHedge, "enqueue_error")
		return nil, fmt.Errorf("enqueue hedge task: %w", err)
	}
	observability.RecordAsyncTask(TypeRunHedge, "enqueued")
	return info, nil
}

type Handler struct {
	service *service.HedgeService
	logger  zerolog.Logger
}

func NewHandler(hedgeService *service.HedgeService, logger zerolog.Logger) *Handler {
	return &Handler{
		service: hedgeService,
		logger:  logger,
	}
}

func (h *Handler) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeRunHedge, h.handleRunHedge)
}

func (h *Handler) handleRunHedge(ctx context.Context, task *asynq.Task) error {
	var payload RunHedgePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		observability.RecordAsyncTask(TypeRunHedge, "bad_payload")
		return fmt.Errorf("unmarshal hedge task payload: %w: %w", err, asynq.SkipRetry)
	}

	input := service.RunInput{
		ConfigID: payload.ConfigID,
		Source:   payload.Source,
		Symbol:   payload.Symbol,
		Reason:   payload.Reason,
	}

	result, err := h.service.RunOnce(ctx, input)
	if err != nil {
		if errors.Is(err, service.ErrRunLocked) {
			observability.RecordAsyncTask(TypeRunHedge, "locked")
			h.logger.Info().Interface("input", input).Msg("hedge task skipped because another run is active")
			return fmt.Errorf("hedge run already locked: %w", asynq.SkipRetry)
		}
		observability.RecordAsyncTask(TypeRunHedge, "run_error")
		h.logger.Error().Err(err).Interface("input", input).Msg("hedge task failed")
		return err
	}

	observability.RecordAsyncTask(TypeRunHedge, "done")
	h.logger.Info().
		Str("symbol", result.Config.Symbol).
		Str("action", string(result.Decision.Action)).
		Str("reason", result.Decision.Reason).
		Msg("hedge task completed")
	return nil
}
