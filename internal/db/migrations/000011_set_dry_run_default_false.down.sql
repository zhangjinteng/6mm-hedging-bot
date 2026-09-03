ALTER TABLE hedge_configs
    ALTER COLUMN dry_run SET DEFAULT true;

COMMENT ON COLUMN hedge_configs.dry_run IS
    '是否只模拟下单，不向交易所真实提交订单。';
