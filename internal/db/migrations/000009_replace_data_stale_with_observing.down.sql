ALTER TABLE hedge_monitor_snapshots
    DROP CONSTRAINT IF EXISTS hedge_monitor_snapshots_health_status_check,
    DROP CONSTRAINT IF EXISTS hedge_monitor_snapshots_status_check;

UPDATE hedge_monitor_snapshots
SET health_status = 'data_stale',
    status = CASE WHEN status = 'observing' THEN 'data_stale' ELSE status END,
    status_reason = CASE
        WHEN status = 'observing' THEN '敞口或交易所仓位快照缺失、已超过有效时间'
        ELSE status_reason
    END
WHERE health_status = 'observing'
   OR status = 'observing';

ALTER TABLE hedge_monitor_snapshots
    ADD CONSTRAINT hedge_monitor_snapshots_health_status_check CHECK (
        health_status IN ('ok', 'account_unavailable', 'data_stale', 'execution_failed')
    ),
    ADD CONSTRAINT hedge_monitor_snapshots_status_check CHECK (
        status IN (
            'global_off',
            'symbol_off',
            'account_unavailable',
            'data_stale',
            'open_required',
            'rebalance_required',
            'exit_required',
            'balanced',
            'execution_failed'
        )
    );
