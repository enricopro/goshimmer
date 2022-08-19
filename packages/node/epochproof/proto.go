package epochproof

import (
	"github.com/cockroachdb/errors"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
	epp "github.com/iotaledger/goshimmer/packages/node/epochproof/epochproofproto"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	"github.com/iotaledger/hive.go/core/identity"
	"google.golang.org/protobuf/proto"
)

func (m *Manager) handlePacket(nbr *p2p.Neighbor, packet proto.Message) error {
	epPacket := packet.(*epp.Packet)
	switch packetBody := epPacket.GetBody().(type) {
	case *epp.Packet_ECSupportersRequest:
		return submitTask(m.processECSupportersRequest, packetBody, nbr)
	case *epp.Packet_ECSupporters:
		return submitTask(m.processECSupporters, packetBody, nbr)
	default:
		return errors.Errorf("unsupported packet; packet=%+v, packetBody=%T-%+v", epPacket, packetBody, packetBody)
	}
}

func (m *Manager) requestECSupporters(ei epoch.Index, ec epoch.EC, to ...identity.ID) {
	supportersReq := &epp.ECSupportersRequest{EI: int64(ei), EC: ec.Bytes()}
	packet := &epp.Packet{Body: &epp.Packet_ECSupportersRequest{ECSupportersRequest: supportersReq}}
	m.p2pManager.Send(packet, protocolID, to...)
	m.log.Debugw("sent EC supporters request", "EI", ei)
}

func (m *Manager) sendECSupporters(supporterBlocks []*tangleold.Block, to ...identity.ID) error {
	epochSupportersElements := make([]*epp.ECSupporter, len(supporterBlocks))

	for i, block := range supporterBlocks {
		blockContentHash, err := block.ContentHash()
		if err != nil {
			return errors.Wrap(err, "failed to get block content hash")
		}
		epochSupportersElements[i] = &epp.ECSupporter{
			IssuerPublicKey:  block.IssuerPublicKey().Bytes(),
			IssuingTime:      block.IssuingTime().Unix(),
			Signature:        block.Signature().Bytes(),
			BlockContentHash: blockContentHash[:],
		}
	}

	packet := &epp.Packet{Body: &epp.Packet_ECSupporters{
		ECSupporters: &epp.ECSupporters{
			ECSupporters: epochSupportersElements,
		},
	}}

	m.p2pManager.Send(packet, protocolID, to...)

	m.log.Debugw("sent epoch supporters")

	return nil
}

func sendNegotiationMessage(ps *p2p.PacketsStream) error {
	packet := &epp.Packet{Body: &epp.Packet_Negotiation{Negotiation: &epp.Negotiation{}}}
	return errors.WithStack(ps.WritePacket(packet))
}

func receiveNegotiationMessage(ps *p2p.PacketsStream) (err error) {
	packet := &epp.Packet{}
	if err = ps.ReadPacket(packet); err != nil {
		return errors.WithStack(err)
	}
	packetBody := packet.GetBody()
	if _, ok := packetBody.(*epp.Packet_Negotiation); !ok {
		return errors.Newf(
			"received packet isn't the negotiation packet; packet=%+v, packetBody=%T-%+v",
			packet, packetBody, packetBody,
		)
	}
	return nil
}

func packetFactory() proto.Message {
	return &epp.Packet{}
}
