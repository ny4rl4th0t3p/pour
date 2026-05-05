package observability

import "github.com/prometheus/client_golang/prometheus"

var (
	DripsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pour_drips_total",
			Help: "Drip outcomes by chain and status (confirmed/failed).",
		},
		[]string{"chain", "status"},
	)

	BatchSizeRecipients = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pour_batch_size_recipients",
			Help:    "Number of recipients per flushed batch.",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250},
		},
		[]string{"chain"},
	)

	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pour_queue_depth",
			Help: "Current request queue depth per distributor.",
		},
		[]string{"chain", "distributor"},
	)

	DistributorRefillTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pour_distributor_refill_total",
			Help: "Number of times a distributor was topped up from the holder.",
		},
		[]string{"chain"},
	)

	DistributorBalance = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pour_distributor_balance",
			Help: "Last observed distributor account balance (base units).",
		},
		[]string{"chain", "distributor", "denom"},
	)

	DistributorRecoveryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pour_distributor_recovery_total",
			Help: "Number of times a distributor entered the recovering state.",
		},
		[]string{"chain"},
	)

	MultisendDisabledTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pour_multisend_disabled_total",
			Help: "Number of times MsgMultiSend was disabled for a chain after repeated failures.",
		},
		[]string{"chain"},
	)

	ChainSuspended = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pour_chain_suspended",
			Help: "1 when the chain is suspended, 0 when healthy.",
		},
		[]string{"chain"},
	)
)

func init() {
	prometheus.MustRegister(
		DripsTotal,
		BatchSizeRecipients,
		QueueDepth,
		DistributorRefillTotal,
		DistributorBalance,
		DistributorRecoveryTotal,
		MultisendDisabledTotal,
		ChainSuspended,
	)
}
