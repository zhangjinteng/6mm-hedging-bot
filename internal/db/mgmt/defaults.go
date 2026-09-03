package mgmt

import "github.com/shopspring/decimal"

func applyHedgeConfigDefaults(config *HedgeConfig) {
	if config.LifecycleStatus == "" {
		if config.Enabled {
			config.LifecycleStatus = HedgeLifecycleActive
		} else {
			config.LifecycleStatus = HedgeLifecycleDisabled
		}
	}
	if config.Source == "" {
		config.Source = "platform"
	}
	if config.TargetSymbol == "" {
		config.TargetSymbol = config.Symbol
	}
	if config.TargetHedgeRatio.IsZero() {
		config.TargetHedgeRatio = decimal.NewFromInt(1)
	}
	if config.FirstTriggerUSDT.IsZero() {
		config.FirstTriggerUSDT = decimal.NewFromInt(5000)
	}
	if config.RebalanceUSDT.IsZero() {
		config.RebalanceUSDT = decimal.NewFromInt(2000)
	}
	if config.ExitUSDT.IsZero() {
		config.ExitUSDT = decimal.NewFromInt(1500)
	}
	if config.MaxSlippageBps == 0 {
		config.MaxSlippageBps = 30
	}
	if config.MinOrderUSDT.IsZero() {
		config.MinOrderUSDT = decimal.NewFromInt(10)
	}
	if config.Leverage == 0 {
		config.Leverage = 1
	}
	if len(config.Metadata) == 0 {
		config.Metadata = []byte("{}")
	}
}
