ALTER TABLE exchange_markets
    ADD COLUMN IF NOT EXISTS normalized_symbol text NOT NULL DEFAULT '';

UPDATE exchange_markets
SET normalized_symbol = upper(regexp_replace(split_part(symbol, ':', 1), '[^A-Za-z0-9]', '', 'g'))
WHERE btrim(normalized_symbol) = '';

WITH duplicated AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY exchange, ccxt_id, exchange_env, market_type, settle_asset, normalized_symbol
               ORDER BY updated_at DESC, id DESC
           ) AS row_number
    FROM exchange_markets
)
DELETE FROM exchange_markets
USING duplicated
WHERE exchange_markets.id = duplicated.id
  AND duplicated.row_number > 1;

ALTER TABLE exchange_markets
    ALTER COLUMN normalized_symbol DROP DEFAULT;

ALTER TABLE exchange_markets
    DROP CONSTRAINT IF EXISTS exchange_markets_normalized_symbol_check;
ALTER TABLE exchange_markets
    ADD CONSTRAINT exchange_markets_normalized_symbol_check CHECK (length(btrim(normalized_symbol)) > 0);

DROP INDEX IF EXISTS exchange_markets_identity_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS exchange_markets_identity_uidx
    ON exchange_markets (exchange, ccxt_id, exchange_env, market_type, settle_asset, normalized_symbol);

CREATE INDEX IF NOT EXISTS exchange_markets_exchange_normalized_symbol_idx
    ON exchange_markets (exchange, ccxt_id, exchange_env, normalized_symbol);

CREATE INDEX IF NOT EXISTS exchange_markets_active_normalized_symbol_idx
    ON exchange_markets (exchange, ccxt_id, exchange_env, active, normalized_symbol);

COMMENT ON COLUMN exchange_markets.normalized_symbol IS '平台归一化交易对，例如 BTCUSDT；用于和 symbol_config 白名单匹配。';
