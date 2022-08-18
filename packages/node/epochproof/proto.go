package epochproof

import (
	"github.com/cockroachdb/errors"
	epp "github.com/iotaledger/goshimmer/packages/node/epochproof/epochproofproto"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
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

func epochproofPacketFactory() proto.Message {
	return &epp.Packet{}
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
