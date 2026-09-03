DROP TRIGGER IF EXISTS hedge_close_requests_set_updated_at ON hedge_close_requests;
DROP TABLE IF EXISTS hedge_close_requests;

ALTER TABLE hedge_configs
    DROP CONSTRAINT IF EXISTS hedge_configs_lifecycle_status_check,
    DROP COLUMN IF EXISTS lifecycle_status;
