package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	hedgingv1 "github.com/zhangjinteng/6mm-hedging-bot/internal/pb/hedging/v1"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/tasks"
)

type Server struct {
	hedgingv1.UnimplementedHedgingServiceServer
	service *service.HedgeService
	queue   *tasks.Enqueuer
}

func NewServer(hedgeService *service.HedgeService, queue *tasks.Enqueuer) *Server {
	return &Server{service: hedgeService, queue: queue}
}

func (s *Server) UpsertExposure(ctx context.Context, req *hedgingv1.UpsertExposureRequest) (*hedgingv1.ExposureSnapshot, error) {
	input := service.UpsertExposureInput{
		AgentID:    req.GetAgentId(),
		Source:     req.GetSource(),
		Symbol:     req.GetSymbol(),
		ObservedAt: timestamp(req.GetObservedAt()),
	}
	var err error
	input.NetQuantity, err = parseDecimal(req.GetNetQuantity(), "net_quantity")
	if err != nil {
		return nil, invalidArgument(err)
	}
	input.NetNotionalUSDT, err = parseDecimal(req.GetNetNotionalUsdt(), "net_notional_usdt")
	if err != nil {
		return nil, invalidArgument(err)
	}
	input.MarkPrice, err = parseDecimal(req.GetMarkPrice(), "mark_price")
	if err != nil {
		return nil, invalidArgument(err)
	}

	exposure, err := s.service.UpsertExposure(ctx, input)
	if err != nil {
		return nil, invalidArgument(err)
	}
	return exposureSnapshot(exposure), nil
}

func (s *Server) UpsertPosition(ctx context.Context, req *hedgingv1.UpsertPositionRequest) (*hedgingv1.PositionSnapshot, error) {
	input := service.UpsertPositionInput{
		AgentID:           req.GetAgentId(),
		ExchangeAccountID: uint(req.GetExchangeAccountId()),
		Exchange:          req.GetExchange(),
		AccountName:       req.GetAccountName(),
		Symbol:            req.GetSymbol(),
		PositionSide:      req.GetPositionSide(),
		ObservedAt:        timestamp(req.GetObservedAt()),
	}
	var err error
	input.Quantity, err = parseDecimal(req.GetQuantity(), "quantity")
	if err != nil {
		return nil, invalidArgument(err)
	}
	input.NotionalUSDT, err = parseDecimal(req.GetNotionalUsdt(), "notional_usdt")
	if err != nil {
		return nil, invalidArgument(err)
	}
	input.EntryPrice, err = parseDecimal(req.GetEntryPrice(), "entry_price")
	if err != nil {
		return nil, invalidArgument(err)
	}
	input.MarkPrice, err = parseDecimal(req.GetMarkPrice(), "mark_price")
	if err != nil {
		return nil, invalidArgument(err)
	}

	position, err := s.service.UpsertPosition(ctx, input)
	if err != nil {
		return nil, invalidArgument(err)
	}
	return positionSnapshot(&position), nil
}

func (s *Server) RunHedge(ctx context.Context, req *hedgingv1.RunHedgeRequest) (*hedgingv1.RunHedgeResponse, error) {
	result, err := s.service.RunOnce(ctx, service.RunInput{
		ConfigID: uint(req.GetConfigId()),
		Source:   req.GetSource(),
		Symbol:   req.GetSymbol(),
	})
	if err != nil {
		if errors.Is(err, service.ErrRunLocked) {
			return nil, status.Error(codes.Aborted, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return runHedgeResponse(result), nil
}

func (s *Server) ExitHedge(ctx context.Context, req *hedgingv1.RunHedgeRequest) (*hedgingv1.RunHedgeResponse, error) {
	result, err := s.service.ExitHedge(ctx, service.ExitInput{
		ConfigID: uint(req.GetConfigId()),
		Source:   req.GetSource(),
		Symbol:   req.GetSymbol(),
	})
	if err != nil {
		if errors.Is(err, service.ErrRunLocked) {
			return nil, status.Error(codes.Aborted, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return runHedgeResponse(result), nil
}

func (s *Server) EnqueueHedgeRun(ctx context.Context, req *hedgingv1.RunHedgeRequest) (*hedgingv1.EnqueueHedgeRunResponse, error) {
	if s.queue == nil {
		return nil, status.Error(codes.FailedPrecondition, "async queue is not configured")
	}
	info, err := s.queue.EnqueueRunHedge(ctx, service.RunInput{
		ConfigID: uint(req.GetConfigId()),
		Source:   req.GetSource(),
		Symbol:   req.GetSymbol(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &hedgingv1.EnqueueHedgeRunResponse{
		Id:       info.ID,
		Queue:    info.Queue,
		Type:     info.Type,
		State:    info.State.String(),
		MaxRetry: int32(info.MaxRetry),
	}, nil
}

func parseDecimal(value, field string) (decimal.Decimal, error) {
	if value == "" {
		return decimal.Zero, nil
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%s must be decimal string: %w", field, err)
	}
	return parsed, nil
}

func invalidArgument(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

func timestamp(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}
