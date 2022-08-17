package epochproof

import (
	epp "github.com/iotaledger/goshimmer/packages/node/epochproof/epochproofproto"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/hive.go/core/identity"
)

// todo finish
func (m *Manager) processECSupportersRequest(packetECSupportersRequest *epp.Packet_EcSupportersRequest, nbr *p2p.Neighbor) {
	//ei := epoch.Index(packetECSupportersRequest.EcSupportersRequest.GetEI())
	//ec := packetECSupportersRequest.EcSupportersRequest.GetEC()
}

// todo finish
func (m *Manager) processECSupporters(packetECSupporters *epp.Packet_EcSupporters, nbr *p2p.Neighbor) {
	for _, supporter := range packetECSupporters.EcSupporters.GetEcSupporter() {
		var nodeID identity.ID
		if err := nodeID.FromBytes(supporter.NodeID); err != nil {
			m.log.Errorf("failed to parse node ID: %v", err)
			return
		}

	}

}
