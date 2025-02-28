package metrics

import (
	"time"

	"github.com/iotaledger/goshimmer/plugins/config"
)

// ParametersDefinition contains the definition of the parameters used by the metrics plugin.
type ParametersDefinition struct {
	// Local defines whether to collect local metrics.
	Local bool `default:"true" usage:"include local metrics"`
	// Global defines whether to collect global metrics.
	Global bool `default:"false" usage:"include global metrics"`
	// ManaUpdateInterval defines interval between mana metrics refreshes.
	ManaUpdateInterval time.Duration `default:"30s" usage:"mana metrics update interval"`
}

// Parameters contains the configuration used by the metrics plugin.
var Parameters = &ParametersDefinition{}

func init() {
	config.BindParameters(Parameters, "metrics")
}
