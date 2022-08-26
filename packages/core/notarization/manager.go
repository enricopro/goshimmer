package notarization

import (
	"sync"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/generics/shrinkingmap"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/iotaledger/hive.go/core/logger"

	"github.com/iotaledger/goshimmer/packages/core/conflictdag"
	"github.com/iotaledger/goshimmer/packages/node/clock"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/ledger"
	"github.com/iotaledger/goshimmer/packages/core/ledger/utxo"
	"github.com/iotaledger/goshimmer/packages/core/ledger/vm/devnetvm"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
)

const (
	defaultMinEpochCommittableAge = 1 * time.Minute
)

// region Manager //////////////////////////////////////////////////////////////////////////////////////////////////////

// Manager is the notarization manager.
type Manager struct {
	tangle                      *tangleold.Tangle
	epochCommitmentFactory      *EpochCommitmentFactory
	epochCommitmentFactoryMutex sync.RWMutex
	bootstrapMutex              sync.RWMutex
	options                     *ManagerOptions
	pendingConflictsCounters    *shrinkingmap.ShrinkingMap[epoch.Index, uint64]
	epochSupportersMutex        sync.RWMutex
	epochSupporters             *shrinkingmap.ShrinkingMap[epoch.Index, map[identity.ID]*tangleold.Block]
	log                         *logger.Logger
	Events                      *Events
	bootstrapped                bool
	sync.RWMutex
}

// NewManager creates and returns a new notarization manager.
func NewManager(epochCommitmentFactory *EpochCommitmentFactory, t *tangleold.Tangle, opts ...ManagerOption) (new *Manager) {
	options := &ManagerOptions{
		MinCommittableEpochAge: defaultMinEpochCommittableAge,
		Log:                    nil,
	}

	for _, option := range opts {
		option(options)
	}

	new = &Manager{
		tangle:                   t,
		epochCommitmentFactory:   epochCommitmentFactory,
		pendingConflictsCounters: shrinkingmap.New[epoch.Index, uint64](),
		epochSupporters:          shrinkingmap.New[epoch.Index, map[identity.ID]*tangleold.Block](),
		log:                      options.Log,
		options:                  options,
		Events: &Events{
			TangleTreeInserted:          event.New[*TangleTreeUpdatedEvent](),
			TangleTreeRemoved:           event.New[*TangleTreeUpdatedEvent](),
			StateMutationTreeInserted:   event.New[*StateMutationTreeUpdatedEvent](),
			StateMutationTreeRemoved:    event.New[*StateMutationTreeUpdatedEvent](),
			UTXOTreeInserted:            event.New[*UTXOUpdatedEvent](),
			UTXOTreeRemoved:             event.New[*UTXOUpdatedEvent](),
			EpochCommittable:            event.New[*EpochCommittableEvent](),
			EpochConfirmed:              event.New[*EpochConfirmedEvent](),
			CompetingCommitmentDetected: event.New[*CompetingCommitmentDetectedEvent](),
			ManaVectorUpdate:            event.New[*ManaVectorUpdateEvent](),
			Bootstrapped:                event.New[*BootstrappedEvent](),
			SyncRange:                   event.New[*SyncRangeEvent](),
			ActivityTreeInserted:        event.New[*ActivityTreeUpdatedEvent](),
			ActivityTreeRemoved:         event.New[*ActivityTreeUpdatedEvent](),
		},
	}

	new.tangle.Storage.Events.BlockStored.Attach(event.NewClosure(func(event *tangleold.BlockStoredEvent) {
		new.onBlockStored(event.Block)
	}))

	new.tangle.ConfirmationOracle.Events().BlockAccepted.Attach(onlyIfBootstrapped(t.TimeManager, func(event *tangleold.BlockAcceptedEvent) {
		new.onBlockAccepted(event.Block)
	}))

	new.tangle.ConfirmationOracle.Events().BlockOrphaned.Attach(onlyIfBootstrapped(t.TimeManager, func(event *tangleold.BlockAcceptedEvent) {
		new.onBlockOrphaned(event.Block)
	}))

	new.tangle.Ledger.Events.TransactionAccepted.Attach(onlyIfBootstrapped(t.TimeManager, func(event *ledger.TransactionAcceptedEvent) {
		new.onTransactionAccepted(event)
	}))

	new.tangle.Ledger.Events.TransactionInclusionUpdated.Attach(onlyIfBootstrapped(t.TimeManager, func(event *ledger.TransactionInclusionUpdatedEvent) {
		new.onTransactionInclusionUpdated(event)
	}))

	new.tangle.Ledger.ConflictDAG.Events.ConflictAccepted.Attach(onlyIfBootstrapped(t.TimeManager, func(event *conflictdag.ConflictAcceptedEvent[utxo.TransactionID]) {
		new.onConflictAccepted(event.ID)
	}))

	new.tangle.Ledger.ConflictDAG.Events.ConflictCreated.Attach(onlyIfBootstrapped(t.TimeManager, func(event *conflictdag.ConflictCreatedEvent[utxo.TransactionID, utxo.OutputID]) {
		new.onConflictCreated(event.ID)
	}))

	new.tangle.Ledger.ConflictDAG.Events.ConflictRejected.Attach(onlyIfBootstrapped(t.TimeManager, func(event *conflictdag.ConflictRejectedEvent[utxo.TransactionID]) {
		new.onConflictRejected(event.ID)
	}))

	new.tangle.TimeManager.Events.AcceptanceTimeUpdated.Attach(onlyIfBootstrapped(t.TimeManager, func(event *tangleold.TimeUpdate) {
		new.onAcceptanceTimeUpdated(event.ATT)
	}))

	new.Events.CompetingCommitmentDetected.Attach(event.NewClosure(func(event *CompetingCommitmentDetectedEvent) {
		// TODO
		//new.onCompetingCommittmentDectected(event.Block)
	}))

	return new
}

func onlyIfBootstrapped[E any](timeManager *tangleold.TimeManager, handler func(event E)) *event.Closure[E] {
	return event.NewClosure(func(event E) {
		if !timeManager.Bootstrapped() {
			return
		}
		handler(event)
	})
}

// StartSnapshot locks the commitment factory and returns the latest ecRecord and last confirmed epoch index.
func (m *Manager) StartSnapshot() (fullEpochIndex epoch.Index, ecRecord *epoch.ECRecord, err error) {
	m.RLock()
	defer m.RUnlock()

	latestConfirmedEpoch, err := m.LatestConfirmedEpochIndex()
	if err != nil {
		return
	}
	ecRecord = m.epochCommitmentFactory.loadECRecord(latestConfirmedEpoch)
	if ecRecord == nil {
		err = errors.Errorf("could not get latest commitment")
		return
	}
	// The snapshottable ledgerstate always sits at latestConfirmedEpoch - snapshotDepth
	fullEpochIndex = latestConfirmedEpoch - epoch.Index(m.epochCommitmentFactory.snapshotDepth)
	if fullEpochIndex < 0 {
		fullEpochIndex = 0
	}
	return
}

// EndSnapshot unlocks the commitment factory when the snapshotting completes.
func (m *Manager) EndSnapshot() {
	m.RUnlock()
}

// LoadOutputsWithMetadata initiates the state and mana trees from a given snapshot.
func (m *Manager) LoadOutputsWithMetadata(outputsWithMetadatas []*ledger.OutputWithMetadata) {
	m.epochCommitmentFactoryMutex.Lock()
	defer m.epochCommitmentFactoryMutex.Unlock()

	for _, outputWithMetadata := range outputsWithMetadatas {
		m.epochCommitmentFactory.storage.ledgerstateStorage.Store(outputWithMetadata).Release()
		err := insertLeaf(m.epochCommitmentFactory.stateRootTree, outputWithMetadata.ID().Bytes(), outputWithMetadata.ID().Bytes())
		if err != nil {
			m.log.Error(err)
		}
		err = m.epochCommitmentFactory.updateManaLeaf(outputWithMetadata, true)
		if err != nil {
			m.log.Error(err)
		}
	}
}

// LoadEpochDiff consumes an epoch diff updating the state tree, mana tree and storing the diff in storage.
func (m *Manager) LoadEpochDiff(epochDiff *ledger.EpochDiff) {
	m.Lock()
	defer m.Unlock()

	for _, spentOutputWithMetadata := range epochDiff.Spent() {
		spentOutputIDBytes := spentOutputWithMetadata.ID().Bytes()
		if has := m.epochCommitmentFactory.storage.ledgerstateStorage.DeleteIfPresent(spentOutputIDBytes); !has {
			if deleted, _ := m.epochCommitmentFactory.removeStateLeaf(spentOutputWithMetadata.ID()); !deleted {
				panic("epoch diff spends an output not contained in the ledger state or could not delete it")
			}
			if err := m.epochCommitmentFactory.updateManaLeaf(spentOutputWithMetadata, false); err != nil {
				panic("could not update mana leaf with spent output")
			}
		}
	}
	for _, createdOutputWithMetadata := range epochDiff.Created() {
		m.epochCommitmentFactory.storage.ledgerstateStorage.Store(createdOutputWithMetadata).Release()
		if err := m.epochCommitmentFactory.insertStateLeaf(createdOutputWithMetadata.ID()); err != nil {
			panic("could not update leaf of state root tree")
		}
		if err := m.epochCommitmentFactory.updateManaLeaf(createdOutputWithMetadata, true); err != nil {
			panic("could not update mana leaf with created output")
		}
	}
}

// LoadECandEIs initiates the ECRecord, latest committable EI, last confirmed EI and acceptance EI from a given snapshot.
func (m *Manager) LoadECandEIs(header *ledger.SnapshotHeader) {
	m.epochCommitmentFactoryMutex.Lock()
	defer m.epochCommitmentFactoryMutex.Unlock()

	// The last committed epoch index corresponds to the last epoch diff stored in the snapshot.
	if err := m.epochCommitmentFactory.storage.setLatestCommittableEpochIndex(header.DiffEpochIndex); err != nil {
		panic("could not set last committed epoch index")
	}

	// We assume as our earliest forking point the last epoch diff stored in the snapshot.
	if err := m.epochCommitmentFactory.storage.setLastConfirmedEpochIndex(header.DiffEpochIndex); err != nil {
		panic("could not set last confirmed epoch index")
	}

	// We set it to the next epoch after snapshotted one. It will be updated upon first confirmed block will arrive.
	if err := m.epochCommitmentFactory.storage.setAcceptanceEpochIndex(header.DiffEpochIndex + 1); err != nil {
		panic("could not set current epoch index")
	}

	m.epochCommitmentFactory.storage.ecRecordStorage.Store(header.LatestECRecord).Release()
}

// LoadActivityLogs loads activity logs from the snapshot and updates the activity tree.
func (m *Manager) LoadActivityLogs(epochActivity epoch.SnapshotEpochActivity) {
	m.epochCommitmentFactoryMutex.Lock()
	defer m.epochCommitmentFactoryMutex.Unlock()

	for ei, nodeActivity := range epochActivity {
		for nodeID, acceptedCount := range nodeActivity.NodesLog() {
			err := m.epochCommitmentFactory.insertActivityLeaf(ei, nodeID, acceptedCount)
			if err != nil {
				m.log.Error(err)
			}
		}
	}
}

// SnapshotEpochDiffs returns the EpochDiffs when a snapshot is created.
func (m *Manager) SnapshotEpochDiffs(fullEpochIndex, latestCommitableEpoch epoch.Index, prodChan chan *ledger.EpochDiff, stopChan chan struct{}) {
	go func() {
		for ei := fullEpochIndex; ei <= latestCommitableEpoch; ei++ {
			spent, created := m.epochCommitmentFactory.loadDiffUTXOs(ei)
			prodChan <- ledger.NewEpochDiff(spent, created)
		}

		close(stopChan)
	}()

	return
}

func (m *Manager) GetEpochDiff(ei epoch.Index) (spent, created []*ledger.OutputWithMetadata) {
	spent, created = m.epochCommitmentFactory.loadDiffUTXOs(ei)
	return
}

// GetEpochDiffs returns the EpochDiffs for the given epoch range, it should be called after acquiring the lock.
func (m *Manager) GetEpochDiffs(from, to epoch.Index) (map[epoch.Index]*ledger.EpochDiff, error) {
	epochDiffsMap := make(map[epoch.Index]*ledger.EpochDiff)
	for ei := from; ei <= to; ei++ {
		spent, created := m.GetEpochDiff(ei)
		epochDiffsMap[ei] = ledger.NewEpochDiff(spent, created)
	}

	return epochDiffsMap, nil
}

// SnapshotLedgerState returns the all confirmed OutputsWithMetadata when a snapshot is created, it should be called in after acquiring the lock.
func (m *Manager) SnapshotLedgerState(prodChan chan *ledger.OutputWithMetadata, stopChan chan struct{}) {
	go func() {
		m.epochCommitmentFactory.loadLedgerState(func(o *ledger.OutputWithMetadata) {
			prodChan <- o
		})
		close(stopChan)
	}()

	return
}

// GetLatestEC returns the latest commitment that a new block should commit to.
func (m *Manager) GetLatestEC() (ecRecord *epoch.ECRecord, err error) {
	m.RLock()
	defer m.RUnlock()

	latestCommittableEpoch, err := m.epochCommitmentFactory.storage.latestCommittableEpochIndex()
	ecRecord = m.epochCommitmentFactory.loadECRecord(latestCommittableEpoch)
	if ecRecord == nil {
		err = errors.Errorf("could not get latest commitment")
	}
	return
}

func (m *Manager) GetECRecord(ei epoch.Index) (ecRecord *epoch.ECRecord, err error) {
	m.RLock()
	defer m.RUnlock()

	ecRecord = m.epochCommitmentFactory.loadECRecord(ei)
	if ecRecord == nil {
		err = errors.Errorf("could not get latest commitment")
	}
	return
}

func (m *Manager) GetEpochSupporters(ei epoch.Index) (map[identity.ID]*tangleold.Block, error) {
	m.epochSupportersMutex.RLock()
	defer m.epochSupportersMutex.RUnlock()

	supporters, exists := m.epochSupporters.Get(ei)
	if !exists {
		return nil, errors.Errorf("could not get epoch supporters")
	}

	return supporters, nil
}

// LatestConfirmedEpochIndex returns the latest epoch index that has been confirmed.
func (m *Manager) LatestConfirmedEpochIndex() (epoch.Index, error) {
	m.RLock()
	defer m.RUnlock()

	return m.epochCommitmentFactory.storage.lastConfirmedEpochIndex()
}

// PendingConflictsCountAll returns the current value of pendingConflictsCount per epoch.
func (m *Manager) PendingConflictsCountAll() (pendingConflicts map[epoch.Index]uint64) {
	pendingConflicts = make(map[epoch.Index]uint64, m.pendingConflictsCounters.Size())
	m.pendingConflictsCounters.ForEach(func(ei epoch.Index, counter uint64) bool {
		pendingConflicts[ei] = counter
		return true
	})
	return pendingConflicts
}

func (m *Manager) SnapshotEpochActivity() (epochActivity epoch.SnapshotEpochActivity) {
	return m.tangle.WeightProvider.SnapshotEpochActivity()
}

// Bootstrapped returns the current value of pendingConflictsCount per epoch.
func (m *Manager) Bootstrapped() bool {
	m.bootstrapMutex.RLock()
	defer m.bootstrapMutex.RUnlock()
	return m.bootstrapped
}

// Shutdown shuts down the manager's permanent storagee.
func (m *Manager) Shutdown() {
	m.Lock()
	defer m.Unlock()

	m.epochCommitmentFactory.storage.shutdown()
}

func (m *Manager) addEpochSupporter(block *tangleold.Block) {
	m.epochSupportersMutex.Lock()
	defer m.epochSupportersMutex.Unlock()

	ei := block.ECRecordEI()
	epochSupporters, exists := m.epochSupporters.Get(ei)
	if !exists {
		epochSupporters = make(map[identity.ID]*tangleold.Block)
		m.epochSupporters.Set(ei, epochSupporters)
	}

	nodeID := identity.NewID(block.IssuerPublicKey())

	if existingBlock, exists := epochSupporters[nodeID]; !exists || block.IssuingTime().After(existingBlock.IssuingTime()) {
		epochSupporters[nodeID] = block
	}
}

/*
func (m *Manager) onCompetingCommittmentDectected(block *tangleold.Block) {
	ei := block.EI()
	m.epochConflicts.

}
*/

// onBlockAccepted is the handler for block confirmed event.
func (m *Manager) onBlockAccepted(block *tangleold.Block) {
	m.Lock()
	defer m.Unlock()

	ei := epoch.IndexFromTime(block.IssuingTime())

	if m.isEpochAlreadyCommitted(ei) {
		m.log.Errorf("block %s accepted with issuing time %s in already committed epoch %d", block.ID(), block.IssuingTime(), ei)
		return
	}
	err := m.epochCommitmentFactory.insertTangleLeaf(ei, block.ID())
	if err != nil && m.log != nil {
		m.log.Error(err)
		return
	}
	m.updateEpochsBootstrapped(ei)

	m.Events.TangleTreeInserted.Trigger(&TangleTreeUpdatedEvent{EI: ei, BlockID: block.ID()})
}

// onBlockStored is a handler fo Block stored event that updates the activity log and triggers warpsyncing.
func (m *Manager) onBlockStored(block *tangleold.Block) {
	m.Lock()
	defer m.Unlock()

	ei := epoch.IndexFromTime(block.IssuingTime())

	nodeID := identity.NewID(block.IssuerPublicKey())
	err := m.epochCommitmentFactory.insertActivityLeaf(ei, nodeID)
	if err != nil && m.log != nil {
		m.log.Error(err)
		return
	}
	m.Events.ActivityTreeInserted.Trigger(&ActivityTreeUpdatedEvent{EI: ei, NodeID: nodeID})

	blockEI := block.ECRecordEI()
	latestCommittableEI := lo.PanicOnErr(m.epochCommitmentFactory.storage.latestCommittableEpochIndex())
	epochDeltaSeconds := time.Duration(int64(blockEI-latestCommittableEI)*epoch.Duration) * time.Second

	m.log.Debugf("block committing to epoch %d stored, latest committable epoch is %d", blockEI, latestCommittableEI)

	// If we are too far behind, we will warpsync
	if epochDeltaSeconds > m.options.BootstrapWindow {
		m.Events.SyncRange.Trigger(&SyncRangeEvent{
			StartEI:   latestCommittableEI,
			EndEI:     blockEI,
			StartEC:   m.epochCommitmentFactory.loadECRecord(latestCommittableEI).ComputeEC(),
			EndPrevEC: block.PrevEC(),
		})
	}

	m.addEpochSupporter(block)
}

// OnBlockOrphaned is the handler for block orphaned event.
func (m *Manager) onBlockOrphaned(block *tangleold.Block) {
	m.epochCommitmentFactoryMutex.Lock()
	defer m.epochCommitmentFactoryMutex.Unlock()

	ei := epoch.IndexFromTime(block.IssuingTime())
	if m.isEpochAlreadyCommitted(ei) {
		m.log.Errorf("block %s orphaned with issuing time %s in already committed epoch %d", block.ID(), block.IssuingTime(), ei)
		return
	}

	if _, err := m.epochCommitmentFactory.removeTangleLeaf(ei, block.ID()); err != nil && m.log != nil {
		m.log.Error(err)
	}

	m.Events.TangleTreeRemoved.Trigger(&TangleTreeUpdatedEvent{EI: ei, BlockID: block.ID()})

	transaction, isTransaction := block.Payload().(utxo.Transaction)
	nodeID := identity.NewID(block.IssuerPublicKey())

	updatedCount := uint64(1)
	// if block has been accepted, counter was increased two times, on booking and on acceptance
	if m.tangle.ConfirmationOracle.IsBlockConfirmed(block.ID()) {
		updatedCount++
	}

	noActivityLeft := m.tangle.WeightProvider.Remove(ei, nodeID, updatedCount)
	if noActivityLeft {
		if _, err := m.epochCommitmentFactory.removeActivityLeaf(ei, nodeID); err != nil && m.log != nil {
			m.log.Error(err)
			return
		}
		m.Events.ActivityTreeRemoved.Trigger(&ActivityTreeUpdatedEvent{EI: ei, NodeID: nodeID})
	}

	if isTransaction {
		spent, created := m.resolveOutputs(transaction)
		m.epochCommitmentFactory.deleteDiffUTXOs(ei, created, spent)
		m.Events.UTXOTreeRemoved.Trigger(&UTXOUpdatedEvent{EI: ei, Spent: spent, Created: created})
	}
}

// onTransactionAccepted is the handler for transaction accepted event.
func (m *Manager) onTransactionAccepted(event *ledger.TransactionAcceptedEvent) {
	m.Lock()
	defer m.Unlock()

	txID := event.TransactionID

	var txInclusionTime time.Time
	m.tangle.Ledger.Storage.CachedTransactionMetadata(txID).Consume(func(txMeta *ledger.TransactionMetadata) {
		txInclusionTime = txMeta.InclusionTime()
	})
	txEpoch := epoch.IndexFromTime(txInclusionTime)

	if m.isEpochAlreadyCommitted(txEpoch) {
		m.log.Errorf("transaction %s accepted with issuing time %s in already committed epoch %d", event.TransactionID, txInclusionTime, txEpoch)
		return
	}

	var spent, created []*ledger.OutputWithMetadata
	m.tangle.Ledger.Storage.CachedTransaction(txID).Consume(func(tx utxo.Transaction) {
		spent, created = m.resolveOutputs(tx)
	})
	if err := m.includeTransactionInEpoch(txID, txEpoch, spent, created); err != nil {
		m.log.Error(err)
	}
}

// onTransactionInclusionUpdated is the handler for transaction inclusion updated event.
func (m *Manager) onTransactionInclusionUpdated(event *ledger.TransactionInclusionUpdatedEvent) {
	m.Lock()
	defer m.Unlock()

	oldEpoch := epoch.IndexFromTime(event.PreviousInclusionTime)
	newEpoch := epoch.IndexFromTime(event.InclusionTime)

	if oldEpoch == 0 || oldEpoch == newEpoch {
		return
	}

	if m.isEpochAlreadyCommitted(oldEpoch) || m.isEpochAlreadyCommitted(newEpoch) {
		m.log.Errorf("inclusion time of transaction changed for already committed epoch: previous EI %d, new EI %d", oldEpoch, newEpoch)
		return
	}

	txID := event.TransactionID

	var spent, created []*ledger.OutputWithMetadata
	m.tangle.Ledger.Storage.CachedTransaction(txID).Consume(func(tx utxo.Transaction) {
		spent, created = m.resolveOutputs(tx)
	})

	if err := m.removeTransactionFromEpoch(txID, oldEpoch, spent, created); err != nil {
		m.log.Error(err)
	}

	if err := m.includeTransactionInEpoch(txID, newEpoch, spent, created); err != nil {
		m.log.Error(err)
	}
}

// onConflictAccepted is the handler for conflict confirmed event.
func (m *Manager) onConflictAccepted(conflictID utxo.TransactionID) {
	epochCommittableEvents, manaVectorUpdateEvents := m.onConflictAcceptedInner(conflictID)
	m.triggerEpochEvents(epochCommittableEvents, manaVectorUpdateEvents)
}

// OnConflictConfirmed is the handler for conflict confirmed event.
func (m *Manager) onConflictAcceptedInner(conflictID utxo.TransactionID) ([]*EpochCommittableEvent, []*ManaVectorUpdateEvent) {
	m.Lock()
	defer m.Unlock()

	ei := m.getConflictEI(conflictID)

	if m.isEpochAlreadyCommitted(ei) {
		m.log.Errorf("conflict confirmed in already committed epoch %d", ei)
		return nil, nil
	}
	return m.decreasePendingConflictCounter(ei)
}

// onConflictCreated is the handler for conflict created event.
func (m *Manager) onConflictCreated(conflictID utxo.TransactionID) {
	m.Lock()
	defer m.Unlock()

	ei := m.getConflictEI(conflictID)

	if m.isEpochAlreadyCommitted(ei) {
		m.log.Errorf("conflict created in already committed epoch %d", ei)
		return
	}
	m.increasePendingConflictCounter(ei)
}

// onConflictRejected is the handler for conflict created event.
func (m *Manager) onConflictRejected(conflictID utxo.TransactionID) {
	epochCommittableEvents, manaVectorUpdateEvents := m.onConflictRejectedInner(conflictID)
	m.triggerEpochEvents(epochCommittableEvents, manaVectorUpdateEvents)
}

// OnConflictRejected is the handler for conflict created event.
func (m *Manager) onConflictRejectedInner(conflictID utxo.TransactionID) ([]*EpochCommittableEvent, []*ManaVectorUpdateEvent) {
	m.Lock()
	defer m.Unlock()

	ei := m.getConflictEI(conflictID)

	if m.isEpochAlreadyCommitted(ei) {
		m.log.Errorf("conflict rejected in already committed epoch %d", ei)
		return nil, nil
	}
	return m.decreasePendingConflictCounter(ei)
}

// onAcceptanceTimeUpdated is the handler for time updated event and triggers the events.
func (m *Manager) onAcceptanceTimeUpdated(newTime time.Time) {
	epochCommittableEvents, manaVectorUpdateEvents := m.onAcceptanceTimeUpdatedInner(newTime)
	m.triggerEpochEvents(epochCommittableEvents, manaVectorUpdateEvents)
}

// OnAcceptanceTimeUpdated is the handler for time updated event and returns events to be triggered.
func (m *Manager) onAcceptanceTimeUpdatedInner(newTime time.Time) ([]*EpochCommittableEvent, []*ManaVectorUpdateEvent) {
	m.Lock()
	defer m.Unlock()

	ei := epoch.IndexFromTime(newTime)
	currentEpochIndex, err := m.epochCommitmentFactory.storage.acceptanceEpochIndex()
	if err != nil {
		m.log.Error(errors.Wrap(err, "could not get current epoch index"))
		return nil, nil
	}
	// moved to the next epoch
	if ei > currentEpochIndex {
		err = m.epochCommitmentFactory.storage.setAcceptanceEpochIndex(ei)
		if err != nil {
			m.log.Error(errors.Wrap(err, "could not set current epoch index"))
			return nil, nil
		}
		return m.moveLatestCommittableEpoch(ei)
	}
	return nil, nil
}

// pendingConflictsCount returns the current value of pendingConflictsCount.
func (m *Manager) pendingConflictsCount(ei epoch.Index) (pendingConflictsCount uint64) {
	pendingConflictsCount, _ = m.pendingConflictsCounters.Get(ei)
	return
}

func (m *Manager) decreasePendingConflictCounter(ei epoch.Index) ([]*EpochCommittableEvent, []*ManaVectorUpdateEvent) {
	count, _ := m.pendingConflictsCounters.Get(ei)
	count--
	m.pendingConflictsCounters.Set(ei, count)

	if count == 0 {
		return m.moveLatestCommittableEpoch(ei)
	}
	return nil, nil
}

func (m *Manager) increasePendingConflictCounter(ei epoch.Index) {
	count, _ := m.pendingConflictsCounters.Get(ei)
	count++
	m.pendingConflictsCounters.Set(ei, count)
}

func (m *Manager) includeTransactionInEpoch(txID utxo.TransactionID, ei epoch.Index, spent, created []*ledger.OutputWithMetadata) (err error) {
	if err := m.epochCommitmentFactory.insertStateMutationLeaf(ei, txID); err != nil {
		return err
	}
	m.epochCommitmentFactory.storeDiffUTXOs(ei, spent, created)

	m.Events.StateMutationTreeInserted.Trigger(&StateMutationTreeUpdatedEvent{TransactionID: txID})
	m.Events.UTXOTreeInserted.Trigger(&UTXOUpdatedEvent{EI: ei, Spent: spent, Created: created})

	return nil
}

func (m *Manager) removeTransactionFromEpoch(txID utxo.TransactionID, ei epoch.Index, spent, created []*ledger.OutputWithMetadata) (err error) {
	if _, err := m.epochCommitmentFactory.removeStateMutationLeaf(ei, txID); err != nil {
		return err
	}
	m.epochCommitmentFactory.deleteDiffUTXOs(ei, spent, created)

	m.Events.StateMutationTreeRemoved.Trigger(&StateMutationTreeUpdatedEvent{TransactionID: txID})
	m.Events.UTXOTreeRemoved.Trigger(&UTXOUpdatedEvent{EI: ei, Spent: spent, Created: created})

	return nil
}

// isCommittable returns if the epoch is committable, if all conflicts are resolved and the epoch is old enough.
func (m *Manager) isCommittable(ei epoch.Index) bool {
	return m.isOldEnough(ei) && m.allPastConflictsAreResolved(ei)
}

func (m *Manager) allPastConflictsAreResolved(ei epoch.Index) (conflictsResolved bool) {
	lastEI, err := m.epochCommitmentFactory.storage.latestCommittableEpochIndex()
	if err != nil {
		return false
	}
	// epoch is not committable if there are any not resolved conflicts in this and past epochs
	for index := lastEI; index <= ei; index++ {
		if count, _ := m.pendingConflictsCounters.Get(index); count != 0 {
			return false
		}
	}
	return true
}

func (m *Manager) isOldEnough(ei epoch.Index, issuingTime ...time.Time) (oldEnough bool) {
	t := ei.EndTime()
	currentATT := m.tangle.TimeManager.ATT()
	if len(issuingTime) > 0 && issuingTime[0].After(currentATT) {
		currentATT = issuingTime[0]
	}

	diff := currentATT.Sub(t)
	if diff < m.options.MinCommittableEpochAge {
		return false
	}
	return true
}

func (m *Manager) getConflictEI(conflictID utxo.TransactionID) (ei epoch.Index) {
	earliestAttachment := m.tangle.BlockFactory.EarliestAttachment(utxo.NewTransactionIDs(conflictID), false)
	ei = epoch.IndexFromTime(earliestAttachment.IssuingTime())
	return
}

func (m *Manager) isEpochAlreadyCommitted(ei epoch.Index) bool {
	latestCommittable, err := m.epochCommitmentFactory.storage.latestCommittableEpochIndex()
	if err != nil {
		m.log.Errorf("could not get the latest committed epoch: %v", err)
		return false
	}
	return ei <= latestCommittable
}

func (m *Manager) resolveOutputs(tx utxo.Transaction) (spentOutputsWithMetadata, createdOutputsWithMetadata []*ledger.OutputWithMetadata) {
	spentOutputsWithMetadata = make([]*ledger.OutputWithMetadata, 0)
	createdOutputsWithMetadata = make([]*ledger.OutputWithMetadata, 0)
	var spentOutputIDs utxo.OutputIDs
	var createdOutputs []utxo.Output

	spentOutputIDs = m.tangle.Ledger.Utils.ResolveInputs(tx.Inputs())
	createdOutputs = tx.(*devnetvm.Transaction).Essence().Outputs().UTXOOutputs()

	for it := spentOutputIDs.Iterator(); it.HasNext(); {
		spentOutputID := it.Next()
		m.tangle.Ledger.Storage.CachedOutput(spentOutputID).Consume(func(spentOutput utxo.Output) {
			m.tangle.Ledger.Storage.CachedOutputMetadata(spentOutputID).Consume(func(spentOutputMetadata *ledger.OutputMetadata) {
				spentOutputsWithMetadata = append(spentOutputsWithMetadata, ledger.NewOutputWithMetadata(spentOutputID, spentOutput, spentOutputMetadata.CreationTime(), spentOutputMetadata.ConsensusManaPledgeID(), spentOutputMetadata.AccessManaPledgeID()))
			})
		})
	}

	for _, createdOutput := range createdOutputs {
		createdOutputID := createdOutput.ID()
		m.tangle.Ledger.Storage.CachedOutputMetadata(createdOutputID).Consume(func(createdOutputMetadata *ledger.OutputMetadata) {
			createdOutputsWithMetadata = append(createdOutputsWithMetadata, ledger.NewOutputWithMetadata(createdOutputID, createdOutput, createdOutputMetadata.CreationTime(), createdOutputMetadata.ConsensusManaPledgeID(), createdOutputMetadata.AccessManaPledgeID()))
		})
	}

	return
}

func (m *Manager) manaVectorUpdate(ei epoch.Index) (event *ManaVectorUpdateEvent) {
	epochForManaVector := ei - epoch.Index(m.options.ManaEpochDelay)
	if epochForManaVector < 1 {
		return
	}
	spent, created := m.epochCommitmentFactory.loadDiffUTXOs(epochForManaVector)
	return &ManaVectorUpdateEvent{
		EI:               ei,
		EpochDiffCreated: created,
		EpochDiffSpent:   spent,
	}
}

func (m *Manager) moveLatestCommittableEpoch(currentEpoch epoch.Index) ([]*EpochCommittableEvent, []*ManaVectorUpdateEvent) {
	latestCommittable, err := m.epochCommitmentFactory.storage.latestCommittableEpochIndex()
	if err != nil {
		m.log.Errorf("could not obtain last committed epoch index: %v", err)
		return nil, nil
	}

	epochCommittableEvents := make([]*EpochCommittableEvent, 0)
	manaVectorUpdateEvents := make([]*ManaVectorUpdateEvent, 0)
	for ei := latestCommittable + 1; ei <= currentEpoch; ei++ {
		if !m.isCommittable(ei) {
			break
		}

		// reads the roots and stores the ec
		// rolls the state trees
		ecRecord, ecRecordErr := m.epochCommitmentFactory.ecRecord(ei)
		if ecRecordErr != nil {
			m.log.Errorf("could not update commitments for epoch %d: %v", ei, ecRecordErr)
			return nil, nil
		}

		if err = m.epochCommitmentFactory.storage.setLatestCommittableEpochIndex(ei); err != nil {
			m.log.Errorf("could not set last committed epoch: %v", err)
			return nil, nil
		}

		// TODO: we pretend that a new epoch always gets confirmed when we advance the latest committable.
		{
			confirmingEpoch := ei - epoch.Index(m.epochCommitmentFactory.confirmationDelay)
			m.epochCommitmentFactory.storage.setLastConfirmedEpochIndex(confirmingEpoch)
			m.Events.EpochConfirmed.Trigger(&EpochConfirmedEvent{
				EI: confirmingEpoch,
			})
		}

		epochCommittableEvents = append(epochCommittableEvents, &EpochCommittableEvent{
			EI:       ei,
			ECRecord: ecRecord,
		})
		if manaVectorUpdateEvent := m.manaVectorUpdate(ei); manaVectorUpdateEvent != nil {
			manaVectorUpdateEvents = append(manaVectorUpdateEvents, manaVectorUpdateEvent)
		}
	}
	return epochCommittableEvents, manaVectorUpdateEvents
}

func (m *Manager) triggerEpochEvents(epochCommittableEvents []*EpochCommittableEvent, manaVectorUpdateEvents []*ManaVectorUpdateEvent) {
	for _, epochCommittableEvent := range epochCommittableEvents {
		m.Events.EpochCommittable.Trigger(epochCommittableEvent)
	}
	for _, manaVectorUpdateEvent := range manaVectorUpdateEvents {
		m.Events.ManaVectorUpdate.Trigger(manaVectorUpdateEvent)
	}
}

func (m *Manager) updateEpochsBootstrapped(ei epoch.Index) {
	if !m.Bootstrapped() && (ei > epoch.IndexFromTime(clock.SyncedTime().Add(-m.options.BootstrapWindow)) || m.options.BootstrapWindow == 0) {
		m.bootstrapMutex.Lock()
		m.bootstrapped = true
		m.bootstrapMutex.Unlock()
		m.Events.Bootstrapped.Trigger(&BootstrappedEvent{})
	}
}

func (m *Manager) GetECChain(startEI, endEI epoch.Index) (ecChain epoch.ECChain, err error) {
	ecChain = make(epoch.ECChain)
	for ei := startEI; ei <= endEI; ei++ {
		ecRecord, ecRecErr := m.GetECRecord(ei)
		if ecRecErr != nil {
			return nil, errors.Wrap(ecRecErr, "could not get EC record")
		}
		ecChain[ei] = ecRecord.ComputeEC()
	}
	return
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

// ManagerOption represents the return type of the optional config parameters of the notarization manager.
type ManagerOption func(options *ManagerOptions)

// ManagerOptions is a container of all the config parameters of the notarization manager.
type ManagerOptions struct {
	MinCommittableEpochAge time.Duration
	BootstrapWindow        time.Duration
	ManaEpochDelay         uint
	Log                    *logger.Logger
}

// MinCommittableEpochAge specifies how old an epoch has to be for it to be committable.
func MinCommittableEpochAge(d time.Duration) ManagerOption {
	return func(options *ManagerOptions) {
		options.MinCommittableEpochAge = d
	}
}

// BootstrapWindow specifies when the notarization manager is considered to be bootstrapped.
func BootstrapWindow(d time.Duration) ManagerOption {
	return func(options *ManagerOptions) {
		options.BootstrapWindow = d
	}
}

// ManaDelay specifies the epoch offset for mana vector from the last committable epoch.
func ManaDelay(d uint) ManagerOption {
	return func(options *ManagerOptions) {
		options.ManaEpochDelay = d
	}
}

// Log provides the logger.
func Log(log *logger.Logger) ManagerOption {
	return func(options *ManagerOptions) {
		options.Log = log
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
