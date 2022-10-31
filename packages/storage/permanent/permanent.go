package permanent

import (
	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/iotaledger/hive.go/core/kvstore"
	"github.com/iotaledger/hive.go/core/types"

	"github.com/iotaledger/goshimmer/packages/core/diskutil"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/protocol/ledger/utxo"
	"github.com/iotaledger/goshimmer/packages/storage/models"
)

type Permanent struct {
	Events *Events

	Settings         *Settings
	Commitments      *Commitments
	UnspentOutputs   *UnspentOutputs
	UnspentOutputIDs *UnspentOutputIDs
	ConsensusWeights *ConsensusWeights
}

func New(disk *diskutil.DiskUtil, unspentOutputsStorage, unspentOutputIDsStore, consensusWeightStore kvstore.KVStore) (newHeaderStorage *Permanent, err error) {
	newHeaderStorage = &Permanent{
		Events:           NewEvents(),
		Settings:         NewSettings(disk.Path("settings.bin")),
		UnspentOutputs:   &UnspentOutputs{unspentOutputsStorage},
		UnspentOutputIDs: NewUnspentOutputIDs(unspentOutputIDsStore),
		ConsensusWeights: NewConsensusWeights(consensusWeightStore),
	}
	if newHeaderStorage.Commitments, err = NewCommitments(disk.Path("commitments.bin")); err != nil {
		return nil, errors.Errorf("failed to create commitments storage: %w", err)
	}

	return newHeaderStorage, nil
}

func (p *Permanent) ApplyEpoch(index epoch.Index, stateDiff *models.MemoryStateDiff) (stateRoot, manaRoot types.Identifier) {
	return p.applyStateDiff(index, stateDiff, p.UnspentOutputIDs.Store, void(p.UnspentOutputIDs.Delete))
}

func (p *Permanent) RollbackEpochStateDiff(index epoch.Index, stateDiff *models.MemoryStateDiff) (stateRoot, manaRoot types.Identifier) {
	return p.applyStateDiff(index, stateDiff, void(p.UnspentOutputIDs.Delete), p.UnspentOutputIDs.Store)
}

func (p *Permanent) ImportUnspentOutputIDs(outputIDs []utxo.OutputID) {
	for _, outputID := range outputIDs {
		p.UnspentOutputIDs.Store(outputID)
	}
}

func (p *Permanent) Shutdown() (err error) {
	return p.Commitments.Close()
}

func (p *Permanent) applyStateDiff(index epoch.Index, stateDiff *models.MemoryStateDiff, create, delete func(id utxo.OutputID)) (stateRoot, manaRoot types.Identifier) {
	for it := stateDiff.CreatedOutputs.Iterator(); it.HasNext(); {
		create(it.Next())
	}
	for it := stateDiff.DeletedOutputs.Iterator(); it.HasNext(); {
		delete(it.Next())
	}

	consensusWeightUpdates := make(map[identity.ID]*models.TimedBalance)

	for id, diff := range stateDiff.ConsensusWeightUpdates {
		if diff == 0 {
			continue
		}

		timedBalance := lo.Return1(p.ConsensusWeights.Load(id))
		if index == timedBalance.LastUpdated {
			continue
		}

		timedBalance.Balance += diff * int64(lo.Compare(index, timedBalance.LastUpdated))
		timedBalance.LastUpdated = index

		consensusWeightUpdates[id] = timedBalance

		if timedBalance.Balance == 0 {
			p.ConsensusWeights.Delete(id)
		} else {
			p.ConsensusWeights.Store(id, timedBalance)
		}
	}

	p.Events.ConsensusWeightsUpdated.Trigger(consensusWeightUpdates)

	return p.UnspentOutputIDs.Root(), p.ConsensusWeights.Root()
}

func void[A, B any](f func(A) B) func(A) {
	return func(a A) { f(a) }
}
