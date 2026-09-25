package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var apiRateLimitRejected = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "api_rate_limit_rejections_total",
	Help: "API requests rejected by rate limit layer and registered route.",
}, []string{"layer", "policy", "route"})

var apiRateLimitSyncFailures = promauto.NewCounter(prometheus.CounterOpts{
	Name: "api_rate_limit_sync_failures_total", Help: "Failed API rate limit configuration refreshes.",
})

var apiRateLimitLastSync = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "api_rate_limit_last_sync_timestamp_seconds", Help: "Last successful API rate limit configuration refresh.",
})

var apiRateLimitRevision = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "api_rate_limit_applied_revision", Help: "API rate limit configuration revision applied by this instance.",
})

func ObserveAPIRateLimitRejection(layer, policy, route string) {
	apiRateLimitRejected.WithLabelValues(layer, policy, route).Inc()
}

func ObserveAPIRateLimitSync(success bool) {
	if success {
		apiRateLimitLastSync.Set(float64(time.Now().Unix()))
	} else {
		apiRateLimitSyncFailures.Inc()
	}
}

func SetAPIRateLimitRevision(revision int64) {
	apiRateLimitRevision.Set(float64(revision))
}
