package epochproof

import (
	"github.com/cockroachdb/errors"
	"github.com/iotaledger/goshimmer/packages/core/notarization"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/goshimmer/packages/node/warpsync"
	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/logger"
	"github.com/iotaledger/hive.go/core/typeutils"
)

const (
	protocolID = "epochproof/0.0.1"
)

type Manager struct {
	p2pManager          *p2p.Manager
	warpSyncManager     *warpsync.Manager
	notarizationManager *notarization.Manager

	log *logger.Logger

	stopped typeutils.AtomicBool
}

func NewManager(p2pManager *p2p.Manager, warpSyncManager *warpsync.Manager, notarizationManager *notarization.Manager,
	log *logger.Logger, opts ...options.Option[Manager]) *Manager {
	m := &Manager{
		p2pManager:          p2pManager,
		warpSyncManager:     warpSyncManager,
		notarizationManager: notarizationManager,
		log:                 log,
	}

	m.p2pManager.RegisterProtocol(protocolID, &p2p.ProtocolHandler{
		PacketFactory:      epochproofPacketFactory,
		NegotiationSend:    sendNegotiationMessage,
		NegotiationReceive: receiveNegotiationMessage,
		PacketHandler:      m.handlePacket,
	})

	options.Apply(m, opts)

	return m
}

func (m *Manager) RequestECSupporters() {

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
