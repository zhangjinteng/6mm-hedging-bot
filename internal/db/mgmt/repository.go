package mgmt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrInvalidConfig = errors.New("invalid configuration")
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateExchangeAccount(ctx context.Context, account *ExchangeAccount) error {
	if err := PrepareExchangeAccountForSave(account); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if account.IsPrimary {
			if err := tx.Model(&ExchangeAccount{}).
				Where("agent_id = ? AND deleted_at IS NULL AND is_primary = ?", account.AgentID, true).
				Update("is_primary", false).Error; err != nil {
				return fmt.Errorf("clear primary exchange accounts: %w", err)
			}
		}
		if err := tx.Create(account).Error; err != nil {
			return fmt.Errorf("create exchange account: %w", err)
		}
		return nil
	})
}

func (r *Repository) ListExchangeAccounts(ctx context.Context) ([]ExchangeAccount, error) {
	var accounts []ExchangeAccount
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("list exchange accounts: %w", err)
	}
	return accounts, nil
}

func (r *Repository) ListActiveExchangeAccounts(ctx context.Context) ([]ExchangeAccount, error) {
	var accounts []ExchangeAccount
	if err := r.db.WithContext(ctx).
		Where("status = ?", ExchangeAccountStatusActive).
		Order("id ASC").
		Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("list active exchange accounts: %w", err)
	}
	return accounts, nil
}

func (r *Repository) GetExchangeAccount(ctx context.Context, id uint) (ExchangeAccount, error) {
	var account ExchangeAccount
	err := r.db.WithContext(ctx).First(&account, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExchangeAccount{}, ErrNotFound
	}
	if err != nil {
		return ExchangeAccount{}, fmt.Errorf("get exchange account: %w", err)
	}
	return account, nil
}

func (r *Repository) FindExchangeAccount(ctx context.Context, exchange, name string) (ExchangeAccount, error) {
	account := ExchangeAccount{Exchange: exchange, Name: name}
	if err := PrepareExchangeAccountForSave(&account); err != nil {
		return ExchangeAccount{}, err
	}

	err := r.db.WithContext(ctx).
		Where("exchange = ? AND name = ?", account.Exchange, account.Name).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExchangeAccount{}, ErrNotFound
	}
	if err != nil {
		return ExchangeAccount{}, fmt.Errorf("find exchange account: %w", err)
	}
	return account, nil
}

// FindUniqueActiveExchangeAccountByIdentity resolves adapter requests without
// putting credentials into asynchronous task payloads. Ambiguous identities
// are rejected because account names are tenant-scoped.
func (r *Repository) FindUniqueActiveExchangeAccountByIdentity(ctx context.Context, exchange, name, ccxtID string) (ExchangeAccount, error) {
	var accounts []ExchangeAccount
	query := r.db.WithContext(ctx).
		Where("LOWER(exchange) = LOWER(?) AND name = ? AND status = ?", strings.TrimSpace(exchange), strings.TrimSpace(name), ExchangeAccountStatusActive)
	if normalized := strings.ToLower(strings.TrimSpace(ccxtID)); normalized != "" {
		query = query.Where("LOWER(ccxt_id) = ?", normalized)
	}
	if err := query.Limit(2).Find(&accounts).Error; err != nil {
		return ExchangeAccount{}, fmt.Errorf("find exchange account by identity: %w", err)
	}
	if len(accounts) == 0 {
		return ExchangeAccount{}, ErrNotFound
	}
	if len(accounts) > 1 {
		return ExchangeAccount{}, fmt.Errorf("%w: exchange account identity is ambiguous", ErrInvalidConfig)
	}
	return accounts[0], nil
}

func (r *Repository) UpdateExchangeAccount(ctx context.Context, id uint, patch ExchangeAccountPatch) (ExchangeAccount, error) {
	var account ExchangeAccount
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("get exchange account: %w", err)
		}
		if err := ApplyExchangeAccountPatch(&account, patch); err != nil {
			return err
		}
		if account.IsPrimary {
			if err := tx.Model(&ExchangeAccount{}).
				Where("agent_id = ? AND deleted_at IS NULL AND id <> ? AND is_primary = ?", account.AgentID, account.ID, true).
				Update("is_primary", false).Error; err != nil {
				return fmt.Errorf("clear primary exchange accounts: %w", err)
			}
		}
		if err := tx.Save(&account).Error; err != nil {
			return fmt.Errorf("update exchange account: %w", err)
		}
		return nil
	})
	if err != nil {
		return ExchangeAccount{}, err
	}
	return account, nil
}

func (r *Repository) DeleteExchangeAccount(ctx context.Context, id uint) error {
	var account ExchangeAccount
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("get exchange account: %w", err)
		}
		if err := tx.Delete(&account).Error; err != nil {
			return fmt.Errorf("delete exchange account: %w", err)
		}
		return nil
	})
	return err
}

func (r *Repository) SetPrimaryExchangeAccount(ctx context.Context, id uint) (ExchangeAccount, error) {
	var account ExchangeAccount
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("get exchange account: %w", err)
		}
		if account.Status == ExchangeAccountStatusDisabled {
			return fmt.Errorf("%w: disabled account cannot be primary", ErrInvalidConfig)
		}
		if err := tx.Model(&ExchangeAccount{}).
			Where("agent_id = ? AND deleted_at IS NULL AND is_primary = ?", account.AgentID, true).
			Update("is_primary", false).Error; err != nil {
			return fmt.Errorf("clear primary exchange accounts: %w", err)
		}
		account.IsPrimary = true
		if err := tx.Save(&account).Error; err != nil {
			return fmt.Errorf("set primary exchange account: %w", err)
		}
		return nil
	})
	if err != nil {
		return ExchangeAccount{}, err
	}
	return account, nil
}

func (r *Repository) CreateHedgeConfig(ctx context.Context, config *HedgeConfig) error {
	applyHedgeConfigDefaults(config)
	if err := r.db.WithContext(ctx).Create(config).Error; err != nil {
		return fmt.Errorf("create hedge config: %w", err)
	}
	return nil
}

func (r *Repository) ListHedgeConfigs(ctx context.Context) ([]HedgeConfig, error) {
	var configs []HedgeConfig
	if err := r.db.WithContext(ctx).
		Preload("ExchangeAccount").
		Order("hedge_configs.id ASC").
		Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("list hedge configs: %w", err)
	}
	return configs, nil
}

func (r *Repository) ListEnabledHedgeConfigs(ctx context.Context) ([]HedgeConfig, error) {
	var configs []HedgeConfig
	if err := r.db.WithContext(ctx).
		Preload("ExchangeAccount").
		Joins("JOIN hedging_settings ON hedging_settings.agent_id = hedge_configs.agent_id AND hedging_settings.enabled = ?", true).
		Joins("JOIN exchange_accounts ON exchange_accounts.id = hedge_configs.exchange_account_id AND exchange_accounts.agent_id = hedge_configs.agent_id").
		Where("hedge_configs.enabled = ?", true).
		Where("hedge_configs.lifecycle_status = ?", HedgeLifecycleActive).
		Where("hedge_configs.deleted_at IS NULL").
		Where("exchange_accounts.deleted_at IS NULL AND exchange_accounts.status = ?", ExchangeAccountStatusActive).
		Order("id ASC").
		Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("list enabled hedge configs: %w", err)
	}
	return configs, nil
}

func (r *Repository) ListHedgeMonitorConfigs(ctx context.Context) ([]HedgeMonitorConfig, error) {
	configs, err := r.ListHedgeConfigs(ctx)
	if err != nil {
		return nil, err
	}

	var settings []HedgingSetting
	if err := r.db.WithContext(ctx).Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("list hedging settings for monitoring: %w", err)
	}
	enabledByAgent := make(map[uint64]bool, len(settings))
	for _, setting := range settings {
		enabledByAgent[setting.AgentID] = setting.Enabled
	}

	result := make([]HedgeMonitorConfig, 0, len(configs))
	for _, config := range configs {
		result = append(result, HedgeMonitorConfig{
			Config:        config,
			GlobalEnabled: enabledByAgent[config.AgentID],
		})
	}
	return result, nil
}

func (r *Repository) GetHedgeConfig(ctx context.Context, id uint) (HedgeConfig, error) {
	var config HedgeConfig
	err := r.db.WithContext(ctx).
		Preload("ExchangeAccount").
		First(&config, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return HedgeConfig{}, ErrNotFound
	}
	if err != nil {
		return HedgeConfig{}, fmt.Errorf("get hedge config: %w", err)
	}
	return config, nil
}

func (r *Repository) FindEnabledHedgeConfig(ctx context.Context, source, symbol string) (HedgeConfig, error) {
	var config HedgeConfig
	err := r.db.WithContext(ctx).
		Preload("ExchangeAccount").
		Where("source = ? AND symbol = ? AND enabled = ? AND lifecycle_status = ?", source, symbol, true, HedgeLifecycleActive).
		Order("id ASC").
		First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return HedgeConfig{}, ErrNotFound
	}
	if err != nil {
		return HedgeConfig{}, fmt.Errorf("find hedge config: %w", err)
	}
	return config, nil
}
