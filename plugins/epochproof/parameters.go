package epochproof

import (
	"github.com/iotaledger/goshimmer/plugins/config"
	"time"
)

// ParametersDefinition contains the definition of configuration parameters used by the gossip plugin.
type ParametersDefinition struct {
	SyncRangeTimeOut time.Duration `default:"30s" usage:"the time after which a sync range is considered as failed"`
}

// Parameters contains the configuration parameters of the gossip plugin.
var Parameters = &ParametersDefinition{}

func init() {
	config.BindParameters(Parameters, "warpsync")
}
