package epochproof

import (
	"time"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
	epp "github.com/iotaledger/goshimmer/packages/node/epochproof/epochproofproto"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/hive.go/core/crypto/ed25519"
)

type supporterProof struct {
	issuerPublicKey  ed25519.PublicKey
	issuingTime      time.Time
	blockContentHash []byte
	signature        ed25519.Signature
}

type supportersProof []*supporterProof

// todo finish
func (m *Manager) processECSupportersRequest(packetECSupportersRequest *epp.Packet_ECSupportersRequest, nbr *p2p.Neighbor) {
	ei := epoch.Index(packetECSupportersRequest.ECSupportersRequest.GetEI())
	ec := epoch.NewMerkleRoot(packetECSupportersRequest.ECSupportersRequest.GetEC())

	ourECRecord, err := m.notarizationManager.GetECRecord(ei)
	if err != nil || ourECRecord.ComputeEC() != ec {
		m.log.Infow("received EC supporters request for unmatched epoch")
		return
	}

	epochSupporters, err := m.notarizationManager.GetEpochSupporters(ei)
	if err != nil {
		m.log.Errorf("failed to get epoch supporters: %v", err)
		return
	}

	mv, err := m.manaVectorForEpoch(ei)
	if err != nil {
		m.log.Errorf("failed to obtain mana vector at epoch %d: %v", ei, err)
		return
	}

	highestSupporters, _, err := mv.GetHighestManaNodes(m.supportersInProof)
	if err != nil {
		m.log.Errorf("failed to obtain highest mana nodes: %v", err)
		return
	}

	supportersBlock := make([]*tangleold.Block, 0)
	for _, supporter := range highestSupporters {
		block, exists := epochSupporters[supporter.ID]
		if !exists {
			continue
		}
		supportersBlock = append(supportersBlock, block)
	}

	m.sendECSupporters(supportersBlock, nbr.ID())
}

func (m *Manager) processECSupporters(packetECSupporters *epp.Packet_ECSupporters, nbr *p2p.Neighbor) {
	m.supportersLock.RLock()
	defer m.supportersLock.RUnlock()

	if !m.supportersInProgress {
		return
	}

	supportersResp := packetECSupporters.ECSupporters.GetECSupporters()
	supporters := make(supportersProof, len(supportersResp))
	for i, supporter := range supportersResp {
		issuerPublicKey, _, err := ed25519.PublicKeyFromBytes(supporter.GetIssuerPublicKey())
		if err != nil {
			m.log.Errorf("failed to parse node ID: %v", err)
			return
		}
		signature, _, err := ed25519.SignatureFromBytes(supporter.GetSignature())
		if err != nil {
			m.log.Errorf("failed to parse signature: %v", err)
			return
		}
		supporters[i] = &supporterProof{
			issuerPublicKey:  issuerPublicKey,
			issuingTime:      time.Unix(supporter.GetIssuingTime(), 0),
			blockContentHash: supporter.BlockContentHash,
			signature:        signature,
		}
	}

	select {
	case <-m.supporterStopChan:
		return
	case m.supportersChan <- supporters:
	}
}
