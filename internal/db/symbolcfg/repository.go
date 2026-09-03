package symbolcfg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEnabledNormalizedSymbols(ctx context.Context) (map[string]struct{}, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("symbol config database is not configured")
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT symbol, base_asset, quote_asset
FROM public.symbol_config
WHERE is_active = 1
  AND deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("query symbol_config: %w", err)
	}
	defer rows.Close()

	symbols := make(map[string]struct{})
	for rows.Next() {
		var symbol string
		var baseAsset string
		var quoteAsset string
		if err := rows.Scan(&symbol, &baseAsset, &quoteAsset); err != nil {
			return nil, fmt.Errorf("scan symbol_config: %w", err)
		}

		normalized := mgmt.NormalizeMarketSymbol(symbol)
		if normalized == "" {
			normalized = mgmt.NormalizeMarketSymbol(baseAsset + quoteAsset)
		}
		if normalized != "" {
			symbols[normalized] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read symbol_config: %w", err)
	}
	return symbols, nil
}
