package blockfactory

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/core/crypto/ed25519"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/identity"

	"github.com/iotaledger/goshimmer/packages/core/commitment"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/blockdag"
	"github.com/iotaledger/goshimmer/packages/protocol/models"
	"github.com/iotaledger/goshimmer/packages/protocol/models/payload"
)

// region Factory ///////////////////////////////////////////////////////////////////////////////////////////////

// Factory acts as a factory to create new blocks.
type Factory struct {
	Events *Events

	// referenceProvider *ReferenceProvider
	identity       *identity.LocalIdentity
	blockRetriever func(blockID models.BlockID) (block *blockdag.Block, exists bool)
	tipSelector    TipSelector
	referencesFunc ReferencesFunc
	commitmentFunc CommitmentFunc

	optsTipSelectionTimeout       time.Duration
	optsTipSelectionRetryInterval time.Duration
}

// NewBlockFactory creates a new block factory.
func NewBlockFactory(localIdentity *identity.LocalIdentity, blockRetriever func(blockID models.BlockID) (block *blockdag.Block, exists bool), tipSelector TipSelectorFunc, referencesFunc ReferencesFunc, commitmentFunc CommitmentFunc, opts ...options.Option[Factory]) *Factory {
	return options.Apply(&Factory{
		Events:         NewEvents(),
		identity:       localIdentity,
		blockRetriever: blockRetriever,
		tipSelector:    tipSelector,
		referencesFunc: referencesFunc,
		commitmentFunc: commitmentFunc,

		optsTipSelectionTimeout:       10 * time.Second,
		optsTipSelectionRetryInterval: 200 * time.Millisecond,
	}, opts)
}

// CreateBlock creates a new block including sequence number and tip selection and returns it.
func (f *Factory) CreateBlock(p payload.Payload, parentsCount ...int) (*models.Block, error) {
	return f.CreateBlockWithReferences(p, nil, parentsCount...)
}

// CreateBlockWithReferences creates a new block with the references submit.
func (f *Factory) CreateBlockWithReferences(p payload.Payload, references models.ParentBlockIDs, strongParentsCountOpt ...int) (*models.Block, error) {
	strongParentsCount := 2
	if len(strongParentsCountOpt) > 0 {
		strongParentsCount = strongParentsCountOpt[0]
	}

	block, err := f.createBlockWithPayload(p, references, strongParentsCount)
	if err != nil {
		return nil, err
	}

	f.Events.BlockConstructed.Trigger(block)
	return block, nil
}

// createBlockWithPayload create a new block. If there are any supplied references, it uses them. Otherwise, uses tip selection.
// It also triggers the BlockConstructed event once it's done, which is for example used by the plugins to listen for
// blocks that shall be attached to the tangle.
func (f *Factory) createBlockWithPayload(p payload.Payload, references models.ParentBlockIDs, strongParentsCount int) (*models.Block, error) {
	payloadBytes, err := p.Bytes()
	if err != nil {
		return nil, errors.Errorf("could not serialize payload: %w", err)
	}

	if payloadLen := len(payloadBytes); payloadLen > payload.MaxSize {
		return nil, errors.Errorf("maximum payload size of %d bytes exceeded", payloadLen)
	}

	epochCommitment, lastConfirmedEpochIndex, err := f.commitmentFunc()
	if err != nil {
		return nil, errors.Errorf("cannot retrieve epoch commitment: %w", err)
	}

	if references.IsEmpty() {
		references, err = f.tryGetReferences(p, strongParentsCount)
		if err != nil {
			return nil, errors.Errorf("error while trying to get references: %w", err)
		}
	}

	block := models.NewBlock(
		models.WithParents(references),
		models.WithIssuer(f.identity.PublicKey()),
		models.WithIssuingTime(f.issuingTime(references)),
		models.WithPayload(p),
		models.WithLatestConfirmedEpoch(lastConfirmedEpochIndex),
		models.WithCommitment(epochCommitment),
		models.WithSignature(ed25519.EmptySignature), // placeholder will be set after signing

		// jUsT4fUn
		models.WithSequenceNumber(1337),
		models.WithNonce(42),
	)

	// create the signature
	signature, err := f.sign(block)
	if err != nil {
		return nil, errors.Errorf("signing failed: %w", err)
	}
	block.SetSignature(signature)

	if err = block.DetermineID(); err != nil {
		return nil, errors.Errorf("there is a problem with the block syntax: %w", err)
	}

	return block, nil
}

func (f *Factory) tryGetReferences(p payload.Payload, parentsCount int) (references models.ParentBlockIDs, err error) {
	references, err = f.getReferences(p, parentsCount)
	if err == nil {
		return references, nil
	}
	f.Events.Error.Trigger(errors.Errorf("could not get references: %w", err))

	timeout := time.NewTimer(f.optsTipSelectionTimeout)
	interval := time.NewTicker(f.optsTipSelectionRetryInterval)
	for {
		select {
		case <-interval.C:
			references, err = f.getReferences(p, parentsCount)
			if err != nil {
				f.Events.Error.Trigger(errors.Errorf("could not get references: %w", err))
				continue
			}

			return references, nil
		case <-timeout.C:
			return nil, errors.Errorf("timeout while trying to select tips and determine references")
		}
	}
}

func (f *Factory) getReferences(p payload.Payload, parentsCount int) (references models.ParentBlockIDs, err error) {
	strongParents := f.tips(p, parentsCount)
	if len(strongParents) == 0 {
		return nil, errors.Errorf("no strong parents were selected in tip selection")
	}

	references, err = f.referencesFunc(p, strongParents)
	// If none of the strong parents are possible references, we have to try again.
	if err != nil {
		return nil, errors.Errorf("references could not be created: %w", err)
	}

	// fill up weak references with weak references to liked missing conflicts
	if _, exists := references[models.WeakParentType]; !exists {
		references[models.WeakParentType] = models.NewBlockIDs()
	}
	// TODO: get from tips conflict tracker
	// references[models.WeakParentType].AddAll(f.referenceProvider.ReferencesToMissingConflicts(issuingTime, models.MaxParentsCount-len(references[models.WeakParentType])))

	// Make sure that there's no duplicate between strong and weak parents.
	references.RemoveDuplicatesFromWeak()

	return references, nil
}

// issuingTime gets the new block's issuing time based on its parents. Due to the monotonicity time checks we must
// ensure that we set the right issuing time (time(block) > time(block's parents).
func (f *Factory) issuingTime(parents models.ParentBlockIDs) time.Time {
	issuingTime := time.Now()

	parents.ForEach(func(parent models.Parent) {
		if parentBlock, exists := f.blockRetriever(parent.ID); exists && !parentBlock.IssuingTime().Before(issuingTime) {
			// TODO: this depends on the time resolution that we serialize to. If nanoseconds we could add a nanosecond.
			issuingTime = parentBlock.IssuingTime().Add(time.Second)
		}
	})

	return issuingTime
}

func (f *Factory) tips(p payload.Payload, parentsCount int) (parents models.BlockIDs) {
	parents = f.tipSelector.Tips(parentsCount)

	// TODO: when Ledger is refactored, we need to rework the stuff below
	// tx, ok := p.(utxo.Transaction)
	// if !ok {
	// 	return parents
	// }

	// If the block is issuing a transaction and is a double spend, we add it in parallel to the earliest attachment
	// to prevent a double spend from being issued in its past cone.
	// if conflictingTransactions := f.tangle.Ledger.Utils.ConflictingTransactions(tx.ID()); !conflictingTransactions.IsEmpty() {
	//	if earliestAttachment := f.EarliestAttachment(conflictingTransactions); earliestAttachment != nil {
	//		return earliestAttachment.ParentsByType(tangle.StrongParentType)
	//	}
	// }

	return parents
}

func (f *Factory) sign(block *models.Block) (ed25519.Signature, error) {
	bytes, err := block.Bytes()
	if err != nil {
		return ed25519.EmptySignature, err
	}

	contentLength := len(bytes) - len(block.Signature())
	return f.identity.Sign(bytes[:contentLength]), nil
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region TipSelector //////////////////////////////////////////////////////////////////////////////////////////////////

// A TipSelector selects two tips, parent2 and parent1, for a new block to attach to.
type TipSelector interface {
	Tips(countParents int) (parents models.BlockIDs)
}

// The TipSelectorFunc type is an adapter to allow the use of ordinary functions as tip selectors.
type TipSelectorFunc func(countParents int) (parents models.BlockIDs)

// Tips calls f().
func (f TipSelectorFunc) Tips(countParents int) (parents models.BlockIDs) {
	return f(countParents)
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region ReferencesFunc ///////////////////////////////////////////////////////////////////////////////////////////////////

// ReferencesFunc is a function type that returns like references a given set of parents of a Block.
type ReferencesFunc func(payload payload.Payload, strongParents models.BlockIDs) (references models.ParentBlockIDs, err error)

// CommitmentFunc is a function type that returns the commitment of the latest committable epoch.
type CommitmentFunc func() (ecRecord *commitment.Commitment, lastConfirmedEpochIndex epoch.Index, err error)

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

func WithTipSelectionTimeout(timeout time.Duration) options.Option[Factory] {
	return func(factory *Factory) {
		factory.optsTipSelectionTimeout = timeout
	}
}

func WithTipSelectionRetryInterval(interval time.Duration) options.Option[Factory] {
	return func(factory *Factory) {
		factory.optsTipSelectionRetryInterval = interval
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
