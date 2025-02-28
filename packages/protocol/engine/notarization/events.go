package notarization

import (
	"github.com/iotaledger/hive.go/core/generics/event"

	"github.com/iotaledger/goshimmer/packages/core/commitment"
)

// Events is a container that acts as a dictionary for the events of the notarization manager.
type Events struct {
	EpochCommitted *event.Linkable[*commitment.Commitment]
	Error          *event.Linkable[error]

	event.LinkableCollection[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.LinkableConstructor(func() (self *Events) {
	return &Events{
		EpochCommitted: event.NewLinkable[*commitment.Commitment](),
		Error:          event.NewLinkable[error](),
	}
})
