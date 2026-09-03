ALTER TABLE hedge_monitor_snapshots
    DROP CONSTRAINT IF EXISTS hedge_monitor_snapshots_health_status_check,
    DROP CONSTRAINT IF EXISTS hedge_monitor_snapshots_status_check;

UPDATE hedge_monitor_snapshots
SET health_status = 'observing',
    status = CASE WHEN status = 'data_stale' THEN 'observing' ELSE status END,
    status_reason = CASE
        WHEN status = 'data_stale' THEN '正在等待最新敞口或交易所仓位快照'
        ELSE status_reason
    END
WHERE health_status = 'data_stale'
   OR status = 'data_stale';

ALTER TABLE hedge_monitor_snapshots
    ADD CONSTRAINT hedge_monitor_snapshots_health_status_check CHECK (
        health_status IN ('ok', 'account_unavailable', 'observing', 'execution_failed')
    ),
    ADD CONSTRAINT hedge_monitor_snapshots_status_check CHECK (
        status IN (
            'global_off',
            'symbol_off',
            'account_unavailable',
            'observing',
            'open_required',
            'rebalance_required',
            'exit_required',
            'balanced',
            'execution_failed'
        )
    );
