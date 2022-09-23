package blockissuer

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/identity"

	"github.com/iotaledger/goshimmer/packages/app/blockissuer/blockfactory"
	"github.com/iotaledger/goshimmer/packages/app/blockissuer/ratesetter"
	"github.com/iotaledger/goshimmer/packages/core/commitment"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/protocol"
	"github.com/iotaledger/goshimmer/packages/protocol/instance/engine"
	"github.com/iotaledger/goshimmer/packages/protocol/instance/engine/congestioncontrol/icca/mana/manamodels"
	"github.com/iotaledger/goshimmer/packages/protocol/instance/engine/congestioncontrol/icca/scheduler"
	"github.com/iotaledger/goshimmer/packages/protocol/instance/engine/tangle/blockdag"
	"github.com/iotaledger/goshimmer/packages/protocol/instance/engine/tangle/booker"
	"github.com/iotaledger/goshimmer/packages/protocol/models"
	"github.com/iotaledger/goshimmer/packages/protocol/models/payload"
)

// region Factory ///////////////////////////////////////////////////////////////////////////////////////////////

// BlockIssuer contains logic to create and issue blocks.
type BlockIssuer struct {
	Events *Events

	*blockfactory.Factory
	*ratesetter.RateSetter
	protocol          *protocol.Protocol
	identity          *identity.LocalIdentity
	referenceProvider *blockfactory.ReferenceProvider

	optsRateSetterOptions   []options.Option[ratesetter.RateSetter]
	optsBlockFactoryOptions []options.Option[blockfactory.Factory]
}

// New creates a new block issuer.
func New(protocol *protocol.Protocol, localIdentity *identity.LocalIdentity, opts ...options.Option[BlockIssuer]) *BlockIssuer {
	return options.Apply(&BlockIssuer{
		Events:            NewEvents(),
		identity:          localIdentity,
		protocol:          protocol,
		referenceProvider: blockfactory.NewReferenceProvider(func() *engine.Engine { return protocol.Instance().Engine }, protocol.Instance().NotarizationManager.LatestCommitableEpochIndex),
	}, opts, func(i *BlockIssuer) {
		i.Factory = blockfactory.NewBlockFactory(
			localIdentity,
			func(blockID models.BlockID) (block *blockdag.Block, exists bool) {
				return i.protocol.Instance().Engine.Tangle.BlockDAG.Block(blockID)
			},
			func(countParents int) (parents models.BlockIDs) {
				return i.protocol.Instance().TipManager.Tips(countParents)
			},
			i.referenceProvider.References,
			func() (ecRecord *commitment.Commitment, lastConfirmedEpochIndex epoch.Index, err error) {
				latestCommitment, err := i.protocol.Instance().NotarizationManager.GetLatestEC()
				if err != nil {
					return nil, 0, err
				}
				confirmedEpochIndex, err := i.protocol.Instance().NotarizationManager.LatestConfirmedEpochIndex()
				if err != nil {
					return nil, 0, err
				}

				return latestCommitment.Commitment(), confirmedEpochIndex, nil
			},
			i.optsBlockFactoryOptions...)

		i.RateSetter = ratesetter.New(
			i.protocol,
			func() map[identity.ID]int64 {
				manaMap, _, err := i.protocol.Instance().Engine.CongestionControl.Tracker.GetManaMap(manamodels.AccessMana)
				if err != nil {
					return make(map[identity.ID]int64)
				}
				return manaMap
			},
			func() int64 {
				totalMana, _, err := i.protocol.Instance().Engine.CongestionControl.Tracker.GetTotalMana(manamodels.AccessMana)
				if err != nil {
					return 0
				}
				return totalMana
			},
			i.identity.ID(),
			i.optsRateSetterOptions...)
	})
}

func (f *BlockIssuer) setupEvents() {
	f.RateSetter.Events.BlockIssued.Attach(event.NewClosure[*models.Block](func(block *models.Block) {
		f.protocol.IssueBlock(block)
	}))
}

// IssuePayload creates a new block including sequence number and tip selection and returns it.
func (f *BlockIssuer) IssuePayload(p payload.Payload, parentsCount ...int) (block *models.Block, err error) {
	if !f.protocol.Instance().IsBootstrapped() {
		return nil, ErrNotBootstraped
	}

	block, err = f.Factory.CreateBlock(p, parentsCount...)
	if err != nil {
		f.Events.Error.Trigger(errors.Errorf("block could not be created: %w", err))
		return block, err
	}

	return block, f.RateSetter.IssueBlock(block)
}

// IssuePayloadWithReferences creates a new block with the references submit.
func (f *BlockIssuer) IssuePayloadWithReferences(p payload.Payload, references models.ParentBlockIDs, strongParentsCountOpt ...int) (block *models.Block, err error) {
	if !f.protocol.Instance().IsBootstrapped() {
		return nil, ErrNotBootstraped
	}

	block, err = f.Factory.CreateBlockWithReferences(p, references, strongParentsCountOpt...)
	if err != nil {
		f.Events.Error.Trigger(errors.Errorf("block with references could not be created: %w", err))
		return nil, err
	}

	return block, f.RateSetter.IssueBlock(block)
}

// IssueBlockAndAwaitBlockToBeBooked awaits maxAwait for the given block to get booked.
func (f *BlockIssuer) IssueBlockAndAwaitBlockToBeBooked(block *models.Block, maxAwait time.Duration) error {
	if !f.protocol.Instance().IsBootstrapped() {
		return ErrNotBootstraped
	}

	// first subscribe to the transaction booked event
	booked := make(chan *booker.Block, 1)
	// exit is used to let the caller exit if for whatever
	// reason the same transaction gets booked multiple times
	exit := make(chan struct{})
	defer close(exit)

	closure := event.NewClosure(func(bookedBlock *booker.Block) {
		if block.ID() != bookedBlock.ID() {
			return
		}
		select {
		case booked <- bookedBlock:
		case <-exit:
		}
	})
	f.protocol.Events.Instance.Engine.Tangle.Booker.BlockBooked.Attach(closure)
	defer f.protocol.Events.Instance.Engine.Tangle.Booker.BlockBooked.Detach(closure)

	err := f.RateSetter.IssueBlock(block)

	if err != nil {
		return errors.Errorf("failed to issue block %s: %w", block.ID().String(), err)
	}

	select {
	case <-time.After(maxAwait):
		return ErrBlockWasNotBookedInTime
	case <-booked:
		return nil
	}
}

// IssueBlockAndAwaitBlockToBeIssued awaits maxAwait for the given block to get issued.
func (f *BlockIssuer) IssueBlockAndAwaitBlockToBeIssued(block *models.Block, maxAwait time.Duration) error {
	if !f.protocol.Instance().IsBootstrapped() {
		return ErrNotBootstraped
	}

	scheduled := make(chan *scheduler.Block, 1)
	exit := make(chan struct{})
	defer close(exit)

	closure := event.NewClosure(func(scheduledBlock *scheduler.Block) {
		if block.ID() != scheduledBlock.ID() {
			return
		}
		select {
		case scheduled <- scheduledBlock:
		case <-exit:
		}
	})
	f.protocol.Events.Instance.Engine.CongestionControl.Scheduler.BlockScheduled.Attach(closure)
	defer f.protocol.Events.Instance.Engine.CongestionControl.Scheduler.BlockScheduled.Detach(closure)

	err := f.RateSetter.IssueBlock(block)

	if err != nil {
		return errors.Errorf("failed to issue block %s: %w", block.ID().String(), err)
	}

	select {
	case <-time.After(maxAwait):
		return ErrBlockWasNotScheduledInTime
	case <-scheduled:
		return nil
	}
}

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

func WithBlockFactoryOptions(blockFactoryOptions []options.Option[blockfactory.Factory]) options.Option[BlockIssuer] {
	return func(issuer *BlockIssuer) {
		issuer.optsBlockFactoryOptions = blockFactoryOptions
	}
}
func WithRateSetterOptions(rateSetterOptions []options.Option[ratesetter.RateSetter]) options.Option[BlockIssuer] {
	return func(issuer *BlockIssuer) {
		issuer.optsRateSetterOptions = rateSetterOptions
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
