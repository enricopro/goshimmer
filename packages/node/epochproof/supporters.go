package epochproof

import (
	epp "github.com/iotaledger/goshimmer/packages/node/epochproof/epochproofproto"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/hive.go/core/crypto/ed25519"
	"github.com/iotaledger/hive.go/core/identity"
	"time"
)

type supporterProof struct {
	nodeID           *identity.ID
	blockContentHash []byte
	timestamp        time.Time
	signature        ed25519.Signature
}

type supportersProof []*supporterProof

// todo finish
func (m *Manager) processECSupportersRequest(packetECSupportersRequest *epp.Packet_ECSupportersRequest, nbr *p2p.Neighbor) {
	//ei := epoch.Index(packetECSupportersRequest.EcSupportersRequest.GetEI())
	//ec := packetECSupportersRequest.EcSupportersRequest.GetECRecord()
}

func (m *Manager) processECSupporters(packetECSupporters *epp.Packet_ECSupporters, nbr *p2p.Neighbor) {
	m.supportersLock.RLock()
	defer m.supportersLock.RUnlock()

	if !m.supportersInProgress {
		return
	}

	supportersResp := packetECSupporters.ECSupporters.GetECSupporter()
	supporters := make(supportersProof, len(supportersResp))
	for i, supporter := range supportersResp {
		nodeID := new(identity.ID)
		if err := nodeID.FromBytes(supporter.NodeID); err != nil {
			m.log.Errorf("failed to parse node ID: %v", err)
			return
		}
		sig, _, err := ed25519.SignatureFromBytes(supporter.Signature)
		if err != nil {
			m.log.Errorf("failed to parse signature: %v", err)
		}
		supporters[i] = &supporterProof{
			nodeID:           nodeID,
			blockContentHash: supporter.BlockContentHash,
			timestamp:        time.Unix(supporter.Timestamp, 0),
			signature:        sig,
		}
	}

	select {
	case <-m.supporterStopChan:
		return
	case m.supportersChan <- &supporters:
	}

}
