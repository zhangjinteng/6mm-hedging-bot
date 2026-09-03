DROP INDEX IF EXISTS exchange_markets_active_normalized_symbol_idx;
DROP INDEX IF EXISTS exchange_markets_exchange_normalized_symbol_idx;

DROP INDEX IF EXISTS exchange_markets_identity_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS exchange_markets_identity_uidx
    ON exchange_markets (exchange, ccxt_id, exchange_env, market_type, settle_asset, symbol);

ALTER TABLE exchange_markets
    DROP CONSTRAINT IF EXISTS exchange_markets_normalized_symbol_check;

ALTER TABLE exchange_markets
    DROP COLUMN IF EXISTS normalized_symbol;
