package epochproof

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/notarization"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/goshimmer/packages/node/warpsync"
	"github.com/iotaledger/hive.go/core/byteutils"
	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/logger"
	"github.com/iotaledger/hive.go/core/serix"
	"github.com/iotaledger/hive.go/core/typeutils"
)

const (
	protocolID = "epochproof/0.0.1"
)

type Manager struct {
	p2pManager          *p2p.Manager
	warpSyncManager     *warpsync.Manager
	notarizationManager *notarization.Manager

	supportersInProof uint

	supportersInProgress bool
	supportersLock       sync.RWMutex
	supportersChan       chan supportersProof
	supporterStopChan    chan struct{}

	competingECCTracker *competingECCTracker

	log *logger.Logger

	stopped typeutils.AtomicBool
}

func NewManager(p2pManager *p2p.Manager, warpSyncManager *warpsync.Manager, notarizationManager *notarization.Manager,
	log *logger.Logger, opts ...options.Option[Manager]) *Manager {
	m := &Manager{
		p2pManager:          p2pManager,
		warpSyncManager:     warpSyncManager,
		notarizationManager: notarizationManager,
		competingECCTracker: NewCompetingChainTracker(),
		log:                 log,
	}

	m.p2pManager.RegisterProtocol(protocolID, &p2p.ProtocolHandler{
		PacketFactory:      packetFactory,
		NegotiationSend:    sendNegotiationMessage,
		NegotiationReceive: receiveNegotiationMessage,
		PacketHandler:      m.handlePacket,
	})

	options.Apply(m, opts)

	return m
}

// WithSupportersInProof allows to set how many top supporters will be returned when proving an epoch's weight.
func WithSupportersInProof(supportersInProof uint) options.Option[Manager] {
	return func(m *Manager) {
		m.supportersInProof = supportersInProof
	}
}

func (m *Manager) RequestECChain(ctx context.Context, ei epoch.Index, competingECRecord *epoch.ECRecord) error {
	m.startRetrievingSupporters()
	defer m.stopRetrievingSupporters()

	latestConfirmedEI, err := m.notarizationManager.LatestConfirmedEpochIndex()
	if err != nil {
		return errors.Wrap(err, "failed to get latest confirmed epoch index")
	}

	confirmedECRecord, err := m.notarizationManager.GetECRecord(latestConfirmedEI)
	if err != nil {
		return errors.Wrap(err, "failed to get latest confirmed EC")
	}

	competingECChain, _, err := m.warpSyncManager.ValidateBackwards(ctx, latestConfirmedEI, ei, confirmedECRecord.ComputeEC(), competingECRecord.PrevEC())
	if err != nil {
		return errors.Wrap(err, "failed to validate competing ECChain")
	}

	forkingPoint, err := m.determineForkingPoint(competingECChain, latestConfirmedEI)
	if err != nil {
		return errors.Wrap(err, "failed to determine forking point")
	}

	forkingManaVector, err := m.manaVectorForEpoch(forkingPoint)
	if err != nil {
		return errors.Wrapf(err, "failed to get mana vector for forking point %d", forkingPoint)
	}
	competingEC := competingECRecord.ComputeEC()
	m.requestECSupporters(competingECRecord.EI(), competingEC)

	select {
	case supportersOfCompetingChain := <-m.supportersChan:
		filteredSupporters := filterValidSupporters(supportersOfCompetingChain, competingEC)
		m.competingECCTracker.updateCompetingECSupporters(filteredSupporters, competingECRecord, competingECChain, latestConfirmedEI)
		targetEpoch := forkingPoint + 1
		ownValidators, err := m.notarizationManager.GetEpochSupporters(targetEpoch)
		if err != nil {
			return errors.Wrapf(err, "failed to get epoch supporters for epoch %d", targetEpoch)
		}
		if isHeavier, err := m.competingECCTracker.isCompetingChainHeavier(targetEpoch, forkingManaVector, ownValidators, competingECChain); !isHeavier {
			return err
		}
		// TODO: reset ledgerstate and warpsync competing chain.
	case <-ctx.Done():
		return errors.Errorf("failed to get supporters proof for epoch %d: %v", ei, ctx.Err())
	}
	return nil
}

func (m *Manager) startRetrievingSupporters() {
	m.supportersLock.Lock()
	defer m.supportersLock.Unlock()
	m.supportersInProgress = true
	m.supportersChan = make(chan supportersProof)
	m.supporterStopChan = make(chan struct{})
}

func (m *Manager) stopRetrievingSupporters() {
	close(m.supporterStopChan)
	m.supportersLock.Lock()
	defer m.supportersLock.Unlock()
	m.supportersInProgress = false
	close(m.supportersChan)
}

func (m *Manager) determineForkingPoint(competingECChain epoch.ECChain, latestConfirmedEI epoch.Index) (forkingPoint epoch.Index, err error) {
	endEI := latestConfirmedEI + epoch.Index(len(competingECChain)) - 1
	ourECChain, err := m.notarizationManager.GetECChain(latestConfirmedEI, endEI)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get latest ECChain")
	}
	for ei := latestConfirmedEI; ; ei++ {
		competingEC, exists := competingECChain[ei]
		if !exists {
			return 0, errors.New("missing EC record in competing ECChain")
		}
		if ourECChain[ei] == competingEC {
			continue
		}
		return ei - 1, nil
	}
}

// IsStopped returns true if the manager is stopped.
func (m *Manager) IsStopped() bool {
	return m.stopped.IsSet()
}

// Stop stops the manager and closes all established connections.
func (m *Manager) Stop() {
	m.stopped.Set()
	m.p2pManager.UnregisterProtocol(protocolID)
}

func submitTask[P any](packetProcessor func(packet P, nbr *p2p.Neighbor), packet P, nbr *p2p.Neighbor) error {
	if added := event.Loop.TrySubmit(func() { packetProcessor(packet, nbr) }); !added {
		return errors.Errorf("WorkerPool full: packet block discarded")
	}
	return nil
}

func filterValidSupporters(supporters supportersProof, confirmedEC epoch.EC) (filteredSupporters supportersProof) {
	filteredSupporters = make(supportersProof, 0)
	for _, supporter := range supporters {
		issuingTimeBytes, err := serix.DefaultAPI.Encode(context.Background(), supporter.issuingTime, serix.WithValidation())
		if err != nil {
			return nil
		}

		if supporter.issuerPublicKey.VerifySignature(byteutils.ConcatBytes(confirmedEC.Bytes(), issuingTimeBytes, supporter.blockContentHash), supporter.signature) {
			filteredSupporters = append(filteredSupporters, supporter)
		}
	}

	return
}
