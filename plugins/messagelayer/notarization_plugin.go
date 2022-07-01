package messagelayer

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/iotaledger/hive.go/daemon"
	"github.com/iotaledger/hive.go/generics/event"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/node"
	"go.uber.org/dig"
	"golang.org/x/crypto/blake2b"

	"github.com/iotaledger/goshimmer/client"
	"github.com/iotaledger/goshimmer/packages/epoch"
	"github.com/iotaledger/goshimmer/packages/jsonmodels"
	"github.com/iotaledger/goshimmer/packages/notarization"
	"github.com/iotaledger/goshimmer/packages/shutdown"
	"github.com/iotaledger/goshimmer/packages/tangle"
)

const (
	// NotarizationPluginName is the name of the notarization plugin.
	NotarizationPluginName = "Notarization"
)

type notarizationDependencies struct {
	dig.In

	Tangle  *tangle.Tangle
	Storage kvstore.KVStore
}

var (
	NotarizationPlugin *node.Plugin
	notarizationDeps   = new(notarizationDependencies)

	notarizationManager *notarization.Manager
)

func init() {
	NotarizationPlugin = node.NewPlugin(NotarizationPluginName, notarizationDeps, node.Enabled, configureNotarizationPlugin, runNotarizationPlugin)

	NotarizationPlugin.Events.Init.Hook(event.NewClosure(func(event *node.InitEvent) {
		if err := event.Container.Provide(newNotarizationManager); err != nil {
			NotarizationPlugin.Panic(err)
		}
	}))
}

func configureNotarizationPlugin(plugin *node.Plugin) {
	notarizationManager = newNotarizationManager(*notarizationDeps)
	if nodeSnapshot != nil {
		notarizationManager.LoadSnapshot(nodeSnapshot.LedgerSnapshot)
	}

	h, _ := blake2b.New256(nil)
	h.Write([]byte(NotarizationParameters.AnchorChildID))
	ChildTangleID := hex.EncodeToString(h.Sum(nil))
	enabled := NotarizationParameters.AnchorEnabled

	c := client.NewGoShimmerAPI(NotarizationParameters.AnchorParentAPI)

	onEpochCommitable := event.NewClosure(func(event *notarization.EpochCommittableEvent) {
		currentRoot := notarizationManager.EpochCommitmentFactory.ECRoots[event.EI].TangleRoot
		if currentRoot.Base58() != "11111111111111111111111111111111" {
			fmt.Printf("################### Epoch Index %d ######################\n", event.EI)
			fmt.Println(notarizationManager.EpochCommitmentFactory.ECRoots[event.EI].TangleRoot.Base58())

			ecr, _, _ := GetLatestEC()

			anchorMsg := &jsonmodels.Anchor{
				Version:        1,
				ChildTangleID:  ChildTangleID,
				LastStampID:    ChildTangleID,
				ChildMessageID: hex.EncodeToString(ecr.ECR().Bytes()),
				MerkleRoot:     hex.EncodeToString(currentRoot.Bytes()),
			}
			if err := c.IssueAnchor(anchorMsg); err != nil {
				Plugin.LogErrorf("error issuing anchor %s", err)
			}
		}
	})

	if enabled {
		notarizationManager.Events.EpochCommittable.Hook(onEpochCommitable)
	}

	// attach mana plugin event after notarization manager has been initialized
	notarizationManager.Events.ManaVectorUpdate.Hook(onManaVectorToUpdateClosure)
}

func runNotarizationPlugin(*node.Plugin) {
	if err := daemon.BackgroundWorker("Notarization", func(ctx context.Context) {
		<-ctx.Done()
		notarizationManager.Shutdown()
	}, shutdown.PriorityNotarization); err != nil {
		NotarizationPlugin.Panicf("Failed to start as daemon: %s", err)
	}
}

func newNotarizationManager(deps notarizationDependencies) *notarization.Manager {
	return notarization.NewManager(
		notarization.NewEpochCommitmentFactory(deps.Storage, deps.Tangle, NotarizationParameters.SnapshotDepth),
		notarizationDeps.Tangle,
		notarization.MinCommittableEpochAge(NotarizationParameters.MinEpochCommitableAge),
		notarization.ManaDelay(ManaParameters.EpochDelay),
		notarization.Log(Plugin.Logger()))
}

// GetLatestEC returns the latest commitment that a new message should commit to.
func GetLatestEC() (ecRecord *epoch.ECRecord, latestConfirmedEpoch epoch.Index, err error) {
	ecRecord, err = notarizationManager.GetLatestEC()
	if err != nil {
		return
	}
	latestConfirmedEpoch, err = notarizationManager.LatestConfirmedEpochIndex()
	return
}
