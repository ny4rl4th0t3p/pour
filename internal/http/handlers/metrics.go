package handlers

import "github.com/prometheus/client_golang/prometheus"

var pourRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "pour_requests_total",
		Help: "POST /v1/pour requests partitioned by chain_id and outcome.",
	},
	[]string{"chain_id", "outcome"},
)

func init() {
	prometheus.MustRegister(pourRequestsTotal)
}
