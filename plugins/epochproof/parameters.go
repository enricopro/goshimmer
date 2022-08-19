package epochproof

import (
	"time"

	"github.com/iotaledger/goshimmer/plugins/config"
)

// ParametersDefinition contains the definition of configuration parameters used by the gossip plugin.
type ParametersDefinition struct {
	// SupportersInProof is the amount of top supporters returned in an epoch proof.
	SupportersInProof uint `default:"10" usage:"the amount of top supporters returned in an epoch proof"`
	// EpochProofTimeOut is the time after which an epoch proof request is considered as failed
	EpochProofTimeOut time.Duration `default:"30s" usage:"the time after which an epoch proof request is considered as failedthe time after which an epoch proof request is considered as failed"`
}

// Parameters contains the configuration parameters of the epochproof plugin.
var Parameters = &ParametersDefinition{}

func init() {
	config.BindParameters(Parameters, "epochproof")
}
