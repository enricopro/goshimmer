package framework

import (
	"fmt"
	"time"

	"github.com/mr-tron/base58"

	"github.com/iotaledger/goshimmer/tools/integration-tests/tester/framework/config"
)

const (
	// ports
	apiPort       = 8080
	dagVizPort    = 8061
	dashboardPort = 8081
	gossipPort    = 14666
	peeringPort   = 14626

	containerNameEntryNode   = "entry_node"
	containerNameReplica     = "replica_"
	containerNameSuffixPumba = "_pumba"

	graceTimePumba = 3 * time.Second

	logsDir             = "/tmp/logs/"
	dockerLogsPrefixLen = 8
)

var (
	// GenesisSeedBytes is the seed of the funds created at genesis.
	GenesisSeedBytes = []byte{
		95, 76, 224, 164, 168, 80, 141, 174, 133, 77, 153, 100, 4, 202, 113, 104,
		71, 130, 88, 200, 46, 56, 243, 121, 216, 236, 70, 146, 234, 158, 206, 230,
	}

	// GenesisTime provides the genesis time for the tests, to start close to epoch 0.
	GenesisTime = time.Now().Add(-time.Minute).Unix()
)

// CreateNetworkConfig is the config for optional plugins passed through NewNetwork.
type CreateNetworkConfig struct {
	// StartSynced specifies whether all node in the network start synced.
	StartSynced bool
	// Autopeering specifies whether autopeering or manual peering is used.
	Autopeering bool
	// Faucet specifies whether the first peer should have the faucet enabled.
	Faucet bool
	// PeerMaster specifies whether the network should include the peer master.
	PeerMaster bool
	// Activity specifies whether nodes schedule activity blocks in regular intervals.
	Activity bool
	// Snapshot to be generated and rendered available for the network.
	Snapshot SnapshotInfo
}

// PeerConfig specifies the default config of a standard GoShimmer peer.
func PeerConfig() config.GoShimmer {
	c := config.NewGoShimmer()

	c.Image = "iotaledger/goshimmer"

	c.DisabledPlugins = []string{"portcheck", "analysisClient", "profiling", "remotelogmetrics", "remotemetrics", "WebAPIEpochEndpoint", "ManaInitializer", "Warpsync"}

	c.Network.Enabled = true

	c.Dashboard.Enabled = true

	c.Dashboard.BindAddress = fmt.Sprintf("0.0.0.0:%d", dashboardPort)

	c.Dagsvisualizer.Enabled = true
	c.Dagsvisualizer.BindAddress = fmt.Sprintf("0.0.0.0:%d", dagVizPort)

	c.P2P.Enabled = true

	c.WebAPI.Enabled = true
	c.WebAPI.BindAddress = fmt.Sprintf(":%d", apiPort)

	c.AutoPeering.Enabled = false
	c.AutoPeering.BindAddress = fmt.Sprintf(":%d", peeringPort)
	c.AutoPeering.EntryNodes = nil

	c.Protocol.Enabled = true
	c.Protocol.Snapshot.Path = "" // snapshot path is set individually in each test
	c.Protocol.GenesisTime = GenesisTime

	c.Notarization.Enabled = true
	c.Notarization.BootstrapWindow = 0 // disable bootstrap window for tests
	c.Notarization.MinEpochCommittableAge = 10 * time.Second

	c.BlockIssuer.Enabled = true
	c.BlockIssuer.RateSetter.Enable = false

	c.Faucet.Enabled = false
	c.Faucet.Seed = base58.Encode(GenesisSeedBytes)
	c.Faucet.PowDifficulty = 1

	c.Activity.Enabled = false
	c.Activity.BroadcastInterval = time.Second // increase frequency to speedup tests

	return c
}

// EntryNodeConfig specifies the default config of a standard GoShimmer entry node.
func EntryNodeConfig() config.GoShimmer {
	c := PeerConfig()

	c.DisabledPlugins = append(c.DisabledPlugins, "metrics",
		"manualpeering", "chat", "WebAPIDataEndpoint", "WebAPIFaucetRequestEndpoint", "WebAPIBlockEndpoint",
		"WebAPIWeightProviderEndpoint", "WebAPIInfoEndpoint", "WebAPIRateSetterEndpoint", "WebAPISchedulerEndpoint", "WebAPIHealthzEndpoint",
		"WebAPIManaEndpoint", "WebAPIEpochEndpoint", "remotelog", "remotelogmetrics", "DAGsVisualizer",
		"Firewall", "WebAPILedgerstateEndpoint", "Warpsync", "retainer", "indexer", "WebAPIManaEndpoint")
	c.P2P.Enabled = false
	c.Activity.Enabled = false
	c.BlockIssuer.Enabled = false
	c.AutoPeering.Enabled = true
	c.Protocol.Enabled = false
	c.Faucet.Enabled = false
	c.Network.Enabled = false
	c.Activity.Enabled = false
	c.Dashboard.Enabled = false
	c.Dagsvisualizer.Enabled = false
	c.Notarization.Enabled = false

	return c
}
