package client

import (
	"io"
	"runtime"
	"time"

	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/shirou/gopsutil/cpu"

	"github.com/iotaledger/goshimmer/packages/app/metrics"
	"github.com/iotaledger/goshimmer/plugins/analysis/packet"
	"github.com/iotaledger/goshimmer/plugins/banner"
)

func sendMetricHeartbeat(w io.Writer, hb *packet.MetricHeartbeat) {
	data, err := packet.NewMetricHeartbeatBlock(hb)
	if err != nil {
		log.Debugw("metric heartbeat block skipped", "err", err)
		return
	}

	if _, err = w.Write(data); err != nil {
		log.Debugw("Error while writing to connection", "Description", err)
	}
	// trigger AnalysisOutboundBytes event
	metrics.Events.AnalysisOutboundBytes.Trigger(&metrics.AnalysisOutboundBytesEvent{AmountBytes: uint64(len(data))})
}

func createMetricHeartbeat() *packet.MetricHeartbeat {
	// get own ID
	nodeID := make([]byte, len(identity.ID{}))
	if deps.Local != nil {
		copy(nodeID, lo.PanicOnErr(deps.Local.ID().Bytes()))
	}

	return &packet.MetricHeartbeat{
		Version: banner.SimplifiedAppVersion,
		OwnID:   nodeID,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		NumCPU:  runtime.GOMAXPROCS(0),
		// TODO: replace this with only the CPU usage of the GoShimmer process.
		CPUUsage: func() (p float64) {
			percent, err := cpu.Percent(time.Second, false)
			if err == nil {
				p = percent[0]
			}
			return
		}(),
		MemoryUsage: func() uint64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.Alloc
		}(),
	}
}
