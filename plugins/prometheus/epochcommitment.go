package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/iotaledger/goshimmer/plugins/metrics"
)

var lastCommittedEpoch prometheus.Gauge

func registerEpochCommittmentMetrics() {
	lastCommittedEpoch = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_committed_ei",
		Help: "Info about last committed epoch Index",
	})
	registry.MustRegister(lastCommittedEpoch)

	// TODO: uncomment when commitments work
	//addCollect(collectEpochCommittmentMetrics)
}

func collectEpochCommittmentMetrics() {
	commitment := metrics.LastCommittedEpoch()
	lastCommittedEpoch.Set(float64(commitment.Index()))
}
