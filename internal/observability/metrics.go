package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	hedgeRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hedging_bot_runs_total",
			Help: "Total hedge run attempts.",
		},
		[]string{"action", "status"},
	)
	hedgeRunDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hedging_bot_run_duration_seconds",
			Help:    "Hedge run duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"action", "status"},
	)
	asyncTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hedging_bot_async_tasks_total",
			Help: "Total async hedge tasks.",
		},
		[]string{"type", "status"},
	)
	schedulerEnqueueTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hedging_bot_scheduler_enqueue_total",
			Help: "Total scheduled enqueue attempts.",
		},
		[]string{"status"},
	)
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hedging_bot_http_requests_total",
			Help: "Total HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hedging_bot_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)
)

func init() {
	prometheus.MustRegister(
		hedgeRunsTotal,
		hedgeRunDuration,
		asyncTasksTotal,
		schedulerEnqueueTotal,
		httpRequestsTotal,
		httpRequestDuration,
	)
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

func RecordHedgeRun(action, status string, duration time.Duration) {
	hedgeRunsTotal.WithLabelValues(action, status).Inc()
	hedgeRunDuration.WithLabelValues(action, status).Observe(duration.Seconds())
}

func RecordAsyncTask(taskType, status string) {
	asyncTasksTotal.WithLabelValues(taskType, status).Inc()
}

func RecordSchedulerEnqueue(status string) {
	schedulerEnqueueTotal.WithLabelValues(status).Inc()
}

func RecordHTTPRequest(method, path, status string, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	httpRequestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
}
