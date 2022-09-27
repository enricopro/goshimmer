package filter

import (
	"github.com/iotaledger/hive.go/core/generics/options"

	"github.com/iotaledger/goshimmer/packages/network/p2p"
	"github.com/iotaledger/goshimmer/packages/protocol/models"
)

type Filter struct {
	Events *Events
}

func New(opts ...options.Option[Filter]) (inbox *Filter) {
	return options.Apply(&Filter{
		Events: NewEvents(),
	}, opts)
}

func (i *Filter) ProcessReceivedBlock(block *models.Block, neighbor *p2p.Neighbor) {
	if !i.blockSignatureValid(block, neighbor) {
		return
	}

	i.Events.BlockReceived.Trigger(block)
}

func (i *Filter) blockSignatureValid(block *models.Block, neighbor *p2p.Neighbor) (success bool) {
	valid, err := block.VerifySignature()
	if err != nil {
		// TODO: TRIGGER ERROR

		return false
	}

	if !valid {
		// TODO: TRIGGER ERROR

		return
	}

	return true
}
