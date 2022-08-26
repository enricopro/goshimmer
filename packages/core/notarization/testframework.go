package notarization

import (
	"github.com/iotaledger/goshimmer/packages/core/conflictdag"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
	"github.com/iotaledger/goshimmer/packages/node/database"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/types"
	"github.com/iotaledger/hive.go/types"
)

const snapshotDepth = 10

type TestFramework struct {
	tangle               *tangleold.Tangle
	notarizationManagers map[string]*Manager
	*tangleold.BlockTestFramework
}

func New() *TestFramework {
	tangle := tangleold.NewTestTangle(tangleold.WithConflictDAGOptions(conflictdag.WithMergeToMaster(false)))
	defer tangle.Shutdown()

	blockTestFramework := tangleold.NewBlockTestFramework(
		tangle,
		tangleold.WithGenesisOutput("G", 3),
	)

	tangle.Setup()

	return &TestFramework{
		tangle:             tangle,
		BlockTestFramework: blockTestFramework,
	}
}

func (t *TestFramework) CreateChain(ei epoch.Index, chainAlias string, opts ...options.Option[EpochTestFrameworkEpochOptions]) (ecRecord *epoch.ECRecord) {
	manager, exists := t.notarizationManagers[chainAlias]
	if !exists {
		db, _ := database.NewMemDB()
		epochCommitmentFactory := NewEpochCommitmentFactory(db.NewStore(), t.tangle, snapshotDepth)
		manager = NewManager(epochCommitmentFactory, t.tangle)
		t.notarizationManagers[chainAlias] = manager
	}

	ecRecord = epoch.NewECRecord(ei)

	epochOptions := NewEpochTestFrameworkEpochOptions()
	options.Apply(epochOptions, opts)

	return ecRecord
}

type EpochTestFrameworkEpochOptions struct {
	blocks map[string]types.Empty
	txs    map[string]types.Empty
}

func NewEpochTestFrameworkEpochOptions() *EpochTestFrameworkEpochOptions {
	return &EpochTestFrameworkEpochOptions{
		blocks: make(map[string]types.Empty),
		txs:    make(map[string]types.Empty),
	}
}

func WithBlocks(blockAliases ...string) options.Option[EpochTestFrameworkEpochOptions] {
	return func(options *EpochTestFrameworkEpochOptions) {
		for _, blockAlias := range blockAliases {
			options.blocks[blockAlias] = types.Void
		}
	}
}

func WithTransactions(txAliases ...string) options.Option[EpochTestFrameworkEpochOptions] {
	return func(options *EpochTestFrameworkEpochOptions) {
		for _, txAlias := range txAliases {
			options.txs[txAlias] = types.Void
		}
	}
}
