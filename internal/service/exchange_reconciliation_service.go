package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/coredb"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
)

type ExchangeReconciliationService struct {
	core             *coredb.Queries
	mgmt             *mgmt.Repository
	exchange         exchange.Adapter
	decodeCredential func(string) (string, error)
}

type ExchangeAccountIdentity struct {
	AccountID   uint
	Exchange    string
	AccountName string
	CCXTID      string
	APIKey      string
}

type ExternalOrderReconciliationInput struct {
	AccountID     uint
	OrderID       string
	ClientOrderID string
	Symbol        string
}

type PositionSettingVerificationInput struct {
	AccountID          uint
	Symbol             string
	ExpectedLeverage   int
	ExpectedMarginMode string
}

func NewExchangeReconciliationService(core *coredb.Queries, repo *mgmt.Repository, adapter exchange.Adapter) *ExchangeReconciliationService {
	return &ExchangeReconciliationService{
		core:             core,
		mgmt:             repo,
		exchange:         adapter,
		decodeCredential: func(value string) (string, error) { return value, nil },
	}
}

func (s *ExchangeReconciliationService) SetCredentialDecoder(decoder func(string) (string, error)) {
	if decoder != nil {
		s.decodeCredential = decoder
	}
}

func (s *ExchangeReconciliationService) RecordScheduleFailure(ctx context.Context, operation, symbol string, err error) {
	if err == nil {
		return
	}
	s.writeAudit(ctx, "exchange.reconcile.schedule", "warn", symbol, err.Error(), map[string]any{
		"operation": operation,
	})
}

func (s *ExchangeReconciliationService) ResolveAccountID(ctx context.Context, identity ExchangeAccountIdentity) (uint, error) {
	var account mgmt.ExchangeAccount
	var err error
	if identity.AccountID > 0 {
		account, err = s.mgmt.GetExchangeAccount(ctx, identity.AccountID)
	} else {
		account, err = s.mgmt.FindUniqueActiveExchangeAccountByIdentity(ctx, identity.Exchange, identity.AccountName, identity.CCXTID)
	}
	if err != nil {
		return 0, err
	}
	if account.Status != mgmt.ExchangeAccountStatusActive {
		return 0, errors.New("exchange account is not active")
	}
	if apiKey := strings.TrimSpace(identity.APIKey); apiKey != "" {
		storedAPIKey, decodeErr := s.decodeCredential(account.APIKeyEncrypted)
		if decodeErr != nil {
			return 0, fmt.Errorf("decrypt exchange API key: %w", decodeErr)
		}
		if strings.TrimSpace(storedAPIKey) != apiKey {
			return 0, errors.New("exchange account API key does not match stored account")
		}
	}
	return account.ID, nil
}

func (s *ExchangeReconciliationService) ReconcileExternalOrder(ctx context.Context, input ExternalOrderReconciliationInput) (string, error) {
	account, accountConfig, err := s.loadAccount(ctx, input.AccountID)
	if err != nil {
		return "", err
	}
	order, err := s.exchange.FetchOrder(ctx, exchange.FetchOrderRequest{
		AccountConfig: accountConfig,
		OrderID:       strings.TrimSpace(input.OrderID),
		ClientOrderID: strings.TrimSpace(input.ClientOrderID),
		Symbol:        strings.TrimSpace(input.Symbol),
	})
	if err != nil {
		return "", err
	}
	status := strings.ToLower(strings.TrimSpace(order.Status))
	s.writeAudit(ctx, "exchange.order.reconcile", auditSeverityForReconciliation(status), input.Symbol, status, map[string]any{
		"account_id":      account.ID,
		"order_id":        order.ExchangeOrderID,
		"client_order_id": order.ClientOrderID,
		"status":          status,
	})
	return status, nil
}

func (s *ExchangeReconciliationService) VerifyPositionSetting(ctx context.Context, input PositionSettingVerificationInput) (bool, string, error) {
	account, accountConfig, err := s.loadAccount(ctx, input.AccountID)
	if err != nil {
		return false, "", err
	}
	positions, err := s.exchange.FetchPositions(ctx, exchange.FetchPositionsRequest{
		AccountConfig: accountConfig,
		Symbols:       []string{input.Symbol},
	})
	if err != nil {
		return false, "", err
	}

	wantedSymbol := normalizeMonitorSymbol(input.Symbol)
	for _, position := range positions {
		if normalizeMonitorSymbol(position.Symbol) != wantedSymbol {
			continue
		}
		if input.ExpectedLeverage > 0 {
			matched := position.Leverage.Equal(decimal.NewFromInt(int64(input.ExpectedLeverage)))
			observed := position.Leverage.String()
			s.writeSettingAudit(ctx, "exchange.leverage.verify", account.ID, input.Symbol, matched, fmt.Sprint(input.ExpectedLeverage), observed)
			return matched, observed, nil
		}
		expected := normalizeMarginMode(input.ExpectedMarginMode)
		observed := normalizeMarginMode(position.MarginMode)
		matched := expected != "" && observed == expected
		s.writeSettingAudit(ctx, "exchange.margin_mode.verify", account.ID, input.Symbol, matched, expected, observed)
		return matched, observed, nil
	}
	return false, "position_not_returned", nil
}

func (s *ExchangeReconciliationService) loadAccount(ctx context.Context, accountID uint) (mgmt.ExchangeAccount, exchange.AccountConfig, error) {
	if accountID == 0 {
		return mgmt.ExchangeAccount{}, exchange.AccountConfig{}, errors.New("exchange account id is required")
	}
	account, err := s.mgmt.GetExchangeAccount(ctx, accountID)
	if err != nil {
		return mgmt.ExchangeAccount{}, exchange.AccountConfig{}, err
	}
	config, err := accountConfigFromStoredAccount(account, s.decodeCredential)
	return account, config, err
}

func (s *ExchangeReconciliationService) writeSettingAudit(ctx context.Context, eventType string, accountID uint, symbol string, matched bool, expected, observed string) {
	severity := "info"
	message := "matched"
	if !matched {
		severity = "warn"
		message = "not_matched"
	}
	s.writeAudit(ctx, eventType, severity, symbol, message, map[string]any{
		"account_id": accountID,
		"expected":   expected,
		"observed":   observed,
	})
}

func (s *ExchangeReconciliationService) writeAudit(ctx context.Context, eventType, severity, symbol, message string, payload any) {
	if s.core == nil {
		return
	}
	_, _ = s.core.CreateAuditEvent(ctx, coredb.CreateAuditEventParams{
		EventType: eventType,
		Severity:  severity,
		Symbol:    symbol,
		Message:   message,
		Payload:   rawJSON(payload),
	})
}

func accountConfigFromStoredAccount(account mgmt.ExchangeAccount, decoder func(string) (string, error)) (exchange.AccountConfig, error) {
	apiKey, err := decoder(account.APIKeyEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt exchange API key: %w", err)
	}
	apiSecret, err := decoder(account.APISecretEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt exchange API secret: %w", err)
	}
	passphrase, err := decoder(account.PassphraseEncrypted)
	if err != nil {
		return exchange.AccountConfig{}, fmt.Errorf("decrypt exchange passphrase: %w", err)
	}
	return exchange.AccountConfig{
		Exchange: account.Exchange, AccountName: account.Name, CCXTID: account.CCXTID,
		MarketType: account.MarketType, Sandbox: account.Sandbox, DefaultSettle: account.DefaultSettle,
		AccountType: account.AccountType, ProductType: account.ProductType, Category: account.Category,
		PositionMode: account.PositionMode, MarginMode: account.MarginMode, RecvWindowMS: account.RecvWindowMS,
		RateLimitMS: account.RateLimitMS, APIKey: apiKey, APISecret: apiSecret, Passphrase: passphrase,
	}, nil
}

func normalizeMarginMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cross", "crossed", "regular_margin":
		return "cross"
	case "isolated", "isolated_margin":
		return "isolated"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func auditSeverityForReconciliation(status string) string {
	if status == exchange.OrderStatusFailed || status == exchange.OrderStatusCanceled {
		return "warn"
	}
	return "info"
}
