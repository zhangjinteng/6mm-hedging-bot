package hedge

import (
	"fmt"

	"github.com/shopspring/decimal"
)

type Action string

const (
	ActionHold      Action = "hold"
	ActionOpen      Action = "open"
	ActionRebalance Action = "rebalance"
	ActionExit      Action = "exit"
	ActionSkip      Action = "skip"
)

const (
	ReasonFirstTrigger         = "first_trigger"
	ReasonRebalance            = "rebalance"
	ReasonExitHedge            = "exit_hedge"
	ReasonManualClose          = "manual_close"
	ReasonHedgeRatioAdjustment = "hedge_ratio_adjustment"
	ReasonPositionFlipClose    = "position_flip_close"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type Config struct {
	ID               int64
	Source           string
	Symbol           string
	TargetSymbol     string
	TargetHedgeRatio decimal.Decimal
	FirstTriggerUSDT decimal.Decimal
	RebalanceUSDT    decimal.Decimal
	ExitUSDT         decimal.Decimal
	MaxSlippageBps   int
	MaxNotionalUSDT  decimal.Decimal
	MinOrderUSDT     decimal.Decimal
	Enabled          bool
	DryRun           bool
}

type Exposure struct {
	Source          string
	Symbol          string
	NetQuantity     decimal.Decimal
	NetNotionalUSDT decimal.Decimal
	MarkPrice       decimal.Decimal
}

type Position struct {
	Exchange     string
	AccountName  string
	Symbol       string
	Quantity     decimal.Decimal
	NotionalUSDT decimal.Decimal
	MarkPrice    decimal.Decimal
	PositionSide string
}

type Decision struct {
	Action              Action          `json:"action"`
	Side                Side            `json:"side,omitempty"`
	Symbol              string          `json:"symbol"`
	TargetNotionalUSDT  decimal.Decimal `json:"target_notional_usdt"`
	CurrentNotionalUSDT decimal.Decimal `json:"current_notional_usdt"`
	AdjustmentUSDT      decimal.Decimal `json:"adjustment_usdt"`
	OrderQuantity       decimal.Decimal `json:"order_quantity"`
	LimitPrice          decimal.Decimal `json:"limit_price"`
	ReduceOnly          bool            `json:"reduce_only"`
	Reason              string          `json:"reason"`
	DryRun              bool            `json:"dry_run"`
}

type Calculator struct{}

func NewCalculator() Calculator {
	return Calculator{}
}

func (Calculator) ClosePosition(config Config, position Position) (Decision, error) {
	zero := decimal.Zero
	symbol := config.TargetSymbol
	if symbol == "" {
		symbol = position.Symbol
	}
	if symbol == "" {
		return Decision{}, fmt.Errorf("target symbol is required")
	}

	markPrice := position.MarkPrice
	if !markPrice.GreaterThan(zero) && !position.Quantity.IsZero() && !position.NotionalUSDT.IsZero() {
		markPrice = position.NotionalUSDT.Abs().Div(position.Quantity.Abs())
	}

	current := position.NotionalUSDT
	if current.IsZero() && !position.Quantity.IsZero() {
		current = position.Quantity.Mul(markPrice)
	}

	decision := Decision{
		Action:              ActionHold,
		Symbol:              symbol,
		TargetNotionalUSDT:  zero,
		CurrentNotionalUSDT: current,
		DryRun:              config.DryRun,
	}
	if current.IsZero() && position.Quantity.IsZero() {
		decision.Reason = "no hedge position to close"
		return decision, nil
	}
	if !markPrice.GreaterThan(zero) {
		return Decision{}, fmt.Errorf("mark price must be greater than zero")
	}

	adjustment := current.Neg()
	decision.Action = ActionExit
	decision.AdjustmentUSDT = adjustment
	decision.ReduceOnly = true
	decision.Reason = ReasonManualClose
	if adjustment.IsPositive() {
		decision.Side = SideBuy
	} else {
		decision.Side = SideSell
	}

	quantity := position.Quantity.Abs()
	if quantity.IsZero() {
		quantity = adjustment.Abs().Div(markPrice)
	}
	decision.OrderQuantity = quantity
	decision.LimitPrice = limitPrice(markPrice, decision.Side, config.MaxSlippageBps)

	return decision, nil
}

func (Calculator) Calculate(config Config, exposure Exposure, position Position) (Decision, error) {
	zero := decimal.Zero
	decision := Decision{
		Action:              ActionHold,
		Symbol:              config.TargetSymbol,
		CurrentNotionalUSDT: position.NotionalUSDT,
		DryRun:              config.DryRun,
	}

	if !config.Enabled {
		decision.Action = ActionSkip
		decision.Reason = "hedge config disabled"
		return decision, nil
	}
	if config.TargetSymbol == "" {
		return Decision{}, fmt.Errorf("target symbol is required")
	}
	if !exposure.MarkPrice.GreaterThan(zero) {
		return Decision{}, fmt.Errorf("mark price must be greater than zero")
	}
	if config.TargetHedgeRatio.IsNegative() {
		return Decision{}, fmt.Errorf("target hedge ratio cannot be negative")
	}

	target := exposure.NetNotionalUSDT.Neg().Mul(config.TargetHedgeRatio)
	if config.MaxNotionalUSDT.GreaterThan(zero) && target.Abs().GreaterThan(config.MaxNotionalUSDT) {
		target = decimal.NewFromInt(int64(target.Sign())).Mul(config.MaxNotionalUSDT)
	}

	absExposure := exposure.NetNotionalUSDT.Abs()
	current := position.NotionalUSDT
	currentlyHedged := !current.IsZero()

	if absExposure.LessThanOrEqual(config.ExitUSDT) {
		if currentlyHedged {
			target = zero
			decision.Action = ActionExit
			decision.Reason = ReasonExitHedge
		} else {
			decision.TargetNotionalUSDT = zero
			decision.Reason = "net exposure is below exit threshold and no hedge exists"
			return decision, nil
		}
	} else if !currentlyHedged {
		if absExposure.LessThan(config.FirstTriggerUSDT) {
			decision.TargetNotionalUSDT = target
			decision.Reason = "net exposure is below first trigger threshold"
			return decision, nil
		}
		decision.Action = ActionOpen
		decision.Reason = ReasonFirstTrigger
	} else {
		decision.Action = ActionRebalance
		decision.Reason = ReasonRebalance
	}

	adjustment := target.Sub(current)
	decision.TargetNotionalUSDT = target
	decision.AdjustmentUSDT = adjustment

	if adjustment.IsZero() {
		decision.Action = ActionHold
		decision.Reason = "current hedge already matches target"
		return decision, nil
	}
	if adjustment.Abs().LessThan(config.MinOrderUSDT) {
		decision.Action = ActionHold
		decision.Reason = "adjustment is below min order notional"
		return decision, nil
	}
	if decision.Action == ActionRebalance && adjustment.Abs().LessThan(config.RebalanceUSDT) {
		decision.Action = ActionHold
		decision.Reason = "adjustment is below rebalance threshold"
		return decision, nil
	}
	if wouldFlipPosition(current, adjustment) {
		closeAdjustment := current.Neg()
		decision.AdjustmentUSDT = closeAdjustment
		decision.ReduceOnly = true
		decision.Reason = ReasonPositionFlipClose
		if closeAdjustment.IsPositive() {
			decision.Side = SideBuy
		} else {
			decision.Side = SideSell
		}
		decision.OrderQuantity = position.Quantity.Abs()
		if decision.OrderQuantity.IsZero() {
			decision.OrderQuantity = closeAdjustment.Abs().Div(exposure.MarkPrice)
		}
		decision.LimitPrice = limitPrice(exposure.MarkPrice, decision.Side, config.MaxSlippageBps)
		return decision, nil
	}

	if adjustment.IsPositive() {
		decision.Side = SideBuy
	} else {
		decision.Side = SideSell
	}
	decision.OrderQuantity = adjustment.Abs().Div(exposure.MarkPrice)
	decision.LimitPrice = limitPrice(exposure.MarkPrice, decision.Side, config.MaxSlippageBps)
	decision.ReduceOnly = shouldReduceOnly(current, adjustment)

	return decision, nil
}

func limitPrice(markPrice decimal.Decimal, side Side, slippageBps int) decimal.Decimal {
	if slippageBps <= 0 {
		return markPrice
	}
	rate := decimal.NewFromInt(int64(slippageBps)).Div(decimal.NewFromInt(10000))
	if side == SideBuy {
		return markPrice.Mul(decimal.NewFromInt(1).Add(rate))
	}
	return markPrice.Mul(decimal.NewFromInt(1).Sub(rate))
}

func shouldReduceOnly(current, adjustment decimal.Decimal) bool {
	if current.IsZero() {
		return false
	}
	if current.Sign() == adjustment.Sign() {
		return false
	}
	return adjustment.Abs().LessThanOrEqual(current.Abs())
}

func wouldFlipPosition(current, adjustment decimal.Decimal) bool {
	if current.IsZero() || current.Sign() == adjustment.Sign() {
		return false
	}
	return adjustment.Abs().GreaterThan(current.Abs())
}
