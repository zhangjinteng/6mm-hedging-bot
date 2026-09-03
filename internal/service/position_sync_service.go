package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

type PositionSyncRepository interface {
	ListHedgeMonitorConfigs(ctx context.Context) ([]mgmt.HedgeMonitorConfig, error)
}

type PositionSnapshotWriter interface {
	UpsertHedgePositionSnapshot(ctx context.Context, arg coredb.UpsertHedgePositionSnapshotParams) (coredb.HedgePositionSnapshot, error)
}

type HedgeMonitorRefresher interface {
	Refresh(ctx context.Context) (HedgeMonitorRefreshResult, error)
}

type PositionSyncService struct {
	repo             PositionSyncRepository
	store            PositionSnapshotWriter
	exchange         exchange.Adapter
	monitor          HedgeMonitorRefresher
	decodeCredential func(string) (string, error)
	now              func() time.Time
	mu               sync.Mutex
}

type PositionSyncFailure struct {
	AgentID     uint64 `json:"agent_id"`
	AccountID   uint   `json:"exchange_account_id"`
	Exchange    string `json:"exchange"`
	AccountName string `json:"account_name"`
	Stage       string `json:"stage"`
	Error       string `json:"error"`
}

type PositionSyncBatchResult struct {
	Accounts  int                       `json:"accounts"`
	Positions int                       `json:"positions"`
	Failures  []PositionSyncFailure     `json:"failures,omitempty"`
	Monitor   HedgeMonitorRefreshResult `json:"monitor"`
}

func NewPositionSyncService(
	repo PositionSyncRepository,
	store PositionSnapshotWriter,
	adapter exchange.Adapter,
	monitor HedgeMonitorRefresher,
) *PositionSyncService {
	return &PositionSyncService{
		repo:             repo,
		store:            store,
		exchange:         adapter,
		monitor:          monitor,
		decodeCredential: func(value string) (string, error) { return value, nil },
		now:              func() time.Time { return time.Now().UTC() },
	}
}

func (s *PositionSyncService) SetCredentialDecoder(decoder func(string) (string, error)) {
	if decoder != nil {
		s.decodeCredential = decoder
	}
}

func (s *PositionSyncService) Sync(ctx context.Context) (PositionSyncBatchResult, error) {
	if s == nil || s.repo == nil || s.store == nil || s.exchange == nil {
		return PositionSyncBatchResult{}, errors.New("position sync service is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.repo.ListHedgeMonitorConfigs(ctx)
	if err != nil {
		return PositionSyncBatchResult{}, fmt.Errorf("list configs for position sync: %w", err)
	}
	accounts := groupPositionSyncAccounts(configs)
	keys := make([]uint, 0, len(accounts))
	for accountID := range accounts {
		keys = append(keys, accountID)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	result := PositionSyncBatchResult{}
	for _, accountID := range keys {
		group := accounts[accountID]
		if group.account.Status != mgmt.ExchangeAccountStatusActive {
			continue
		}
		result.Accounts++
		if err := s.syncAccount(ctx, group); err != nil {
			result.Failures = append(result.Failures, PositionSyncFailure{
				AgentID:     group.account.AgentID,
				AccountID:   group.account.ID,
				Exchange:    group.account.Exchange,
				AccountName: group.account.Name,
				Stage:       "fetch_positions",
				Error:       err.Error(),
			})
			continue
		}
		result.Positions += len(group.symbols)
	}

	if s.monitor != nil {
		monitorResult, err := s.monitor.Refresh(ctx)
		if err != nil {
			return result, fmt.Errorf("refresh hedge monitor after position sync: %w", err)
		}
		result.Monitor = monitorResult
	}
	return result, nil
}

type positionSyncAccountGroup struct {
	account mgmt.ExchangeAccount
	symbols map[string]string
}

func groupPositionSyncAccounts(configs []mgmt.HedgeMonitorConfig) map[uint]positionSyncAccountGroup {
	result := make(map[uint]positionSyncAccountGroup)
	for _, monitorConfig := range configs {
		config := monitorConfig.Config
		account := config.ExchangeAccount
		if account.ID == 0 || account.AgentID != config.AgentID {
			continue
		}
		target := strings.TrimSpace(config.TargetSymbol)
		normalized := normalizeMonitorSymbol(target)
		if normalized == "" {
			continue
		}
		group := result[account.ID]
		group.account = account
		if group.symbols == nil {
			group.symbols = make(map[string]string)
		}
		group.symbols[normalized] = target
		result[account.ID] = group
	}
	return result
}

func (s *PositionSyncService) syncAccount(ctx context.Context, group positionSyncAccountGroup) error {
	accountConfig, err := positionSyncAccountConfig(group.account, s.decodeCredential)
	if err != nil {
		return err
	}
	symbols := make([]string, 0, len(group.symbols))
	for _, symbol := range group.symbols {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	positions, err := s.exchange.FetchPositions(ctx, exchange.FetchPositionsRequest{
		AccountConfig: accountConfig,
		Symbols:       symbols,
	})
	if err != nil {
		return err
	}

	now := s.now()
	aggregates := make(map[string]positionAggregate, len(group.symbols))
	for normalized, symbol := range group.symbols {
		aggregates[normalized] = positionAggregate{symbol: symbol, observedAt: now}
	}
	for _, position := range positions {
		normalized := normalizeMonitorSymbol(position.Symbol)
		aggregate, configured := aggregates[normalized]
		if !configured {
			continue
		}
		quantity, notional := normalizeExchangePositionValues(position)
		aggregate.quantity = aggregate.quantity.Add(quantity)
		aggregate.notional = aggregate.notional.Add(notional)
		if position.EntryPrice.GreaterThan(decimal.Zero) {
			aggregate.entryPrice = position.EntryPrice
		}
		if position.MarkPrice.GreaterThan(decimal.Zero) {
			aggregate.markPrice = position.MarkPrice
		}
		if position.UpdatedAt.After(aggregate.observedAt) {
			aggregate.observedAt = position.UpdatedAt
		}
		aggregates[normalized] = aggregate
	}

	for _, aggregate := range aggregates {
		if _, err := s.store.UpsertHedgePositionSnapshot(ctx, coredb.UpsertHedgePositionSnapshotParams{
			AgentID:           int64(group.account.AgentID),
			ExchangeAccountID: int64(group.account.ID),
			Exchange:          group.account.Exchange,
			AccountName:       group.account.Name,
			Symbol:            aggregate.symbol,
			PositionSide:      "NET",
			Quantity:          aggregate.quantity,
			NotionalUsdt:      aggregate.notional,
			EntryPrice:        aggregate.entryPrice,
			MarkPrice:         aggregate.markPrice,
			ObservedAt:        aggregate.observedAt,
		}); err != nil {
			return fmt.Errorf("upsert position %s: %w", aggregate.symbol, err)
		}
	}
	return nil
}

type positionAggregate struct {
	symbol     string
	quantity   decimal.Decimal
	notional   decimal.Decimal
	entryPrice decimal.Decimal
	markPrice  decimal.Decimal
	observedAt time.Time
}

func positionSyncAccountConfig(account mgmt.ExchangeAccount, decoder func(string) (string, error)) (exchange.AccountConfig, error) {
	config := ExchangeAccountConfigFromModel(account)
	var err error
	config.APIKey, err = decoder(account.APIKeyEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt API key: %w", err)
	}
	config.APISecret, err = decoder(account.APISecretEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt API secret: %w", err)
	}
	config.Passphrase, err = decoder(account.PassphraseEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt passphrase: %w", err)
	}
	return config, nil
}

func signedExchangePositionQuantity(position exchange.Position) decimal.Decimal {
	switch strings.ToUpper(strings.TrimSpace(position.PositionSide)) {
	case "SHORT":
		return position.Quantity.Abs().Neg()
	case "LONG":
		return position.Quantity.Abs()
	default:
		return position.Quantity
	}
}

func normalizeExchangePositionValues(position exchange.Position) (decimal.Decimal, decimal.Decimal) {
	quantity := signedExchangePositionQuantity(position)
	return quantity, signedExchangePositionNotional(position, quantity)
}

func signedExchangePositionNotional(position exchange.Position, signedQuantity decimal.Decimal) decimal.Decimal {
	notional := position.NotionalUSDT
	if notional.IsZero() && position.MarkPrice.GreaterThan(decimal.Zero) {
		notional = signedQuantity.Abs().Mul(position.MarkPrice)
	}
	if signedQuantity.IsNegative() {
		return notional.Abs().Neg()
	}
	if signedQuantity.IsPositive() {
		return notional.Abs()
	}
	return decimal.Zero
}
