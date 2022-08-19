package epochproof

import (
	"context"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/notarization"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
	"github.com/iotaledger/goshimmer/packages/node/epochproof"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/goshimmer/packages/node/shutdown"
	"github.com/iotaledger/goshimmer/packages/node/warpsync"
	"github.com/iotaledger/hive.go/core/daemon"
	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/iotaledger/hive.go/core/node"
	"go.uber.org/dig"
)

// PluginName is the name of the epochproof plugin.
const PluginName = "Epochproof"

var (
	// Plugin is the plugin instance of the warpsync plugin.
	Plugin *node.Plugin

	deps = new(dependencies)
)

type dependencies struct {
	dig.In

	Tangle          *tangleold.Tangle
	EpochProofMgr   *epochproof.Manager
	WarpsyncMgr     *warpsync.Manager
	NotarizationMgr *notarization.Manager
	P2PMgr          *p2p.Manager
}

func init() {
	Plugin = node.NewPlugin(PluginName, deps, node.Enabled, configure, run)

	Plugin.Events.Init.Hook(event.NewClosure(func(event *node.InitEvent) {
		if err := event.Container.Provide(func(t *tangleold.Tangle, p2pManager *p2p.Manager) *epochproof.Manager {
			return epochproof.NewManager(p2pManager, deps.WarpsyncMgr, deps.NotarizationMgr, Plugin.Logger(), epochproof.WithSupportersInProof(Parameters.SupportersInProof))
		}); err != nil {
			Plugin.Panic(err)
		}
	}))
}

func configure(_ *node.Plugin) {
	deps.Tangle.Storage.Events.BlockStored.Attach(event.NewClosure(func(block *tangleold.BlockStoredEvent) {
		ei := block.Block.EI()

		otherEC := epoch.NewECRecord(ei)
		otherEC.SetECR(block.Block.ECR())
		otherEC.SetPrevEC(block.Block.PrevEC())
		ourEC, err := deps.NotarizationMgr.GetECRecord(ei)
		// ignore the encountered commitment as it is either far in the pas, or we don't have this epoch committable yet
		if err != nil {
			return
		}
		otherNodeID := identity.New(block.Block.IssuerPublicKey())
		if ourEC.ComputeEC() == otherEC.ComputeEC() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), Parameters.EpochProofTimeOut)
		defer cancel()
		deps.EpochProofMgr.RequestECChain(ctx, ei, otherNodeID, otherEC)

	}))
}

func start(ctx context.Context) {
	defer Plugin.LogInfo("Stopping " + PluginName + " ... done")
	<-ctx.Done()
	Plugin.LogInfo("Stopping " + PluginName + " ...")
}

func run(plugin *node.Plugin) {
	if err := daemon.BackgroundWorker(PluginName, start, shutdown.PriorityEpochproof); err != nil {
		plugin.Logger().Panicf("Failed to start as daemon: %s", err)
	}
}
