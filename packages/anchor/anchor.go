package anchor

import (
	"time"

	"github.com/iotaledger/goshimmer/packages/jsonmodels"
	"github.com/iotaledger/hive.go/events"
	"github.com/mr-tron/base58"
)

// NewAnchor creates a new Anchor.
func NewAnchor() *Anchor {
	return &Anchor{
		Events: Events{
			MessageReceived: events.NewEvent(chatEventCaller),
		},
	}
}

// Anchor manages anchors happening over the Tangle.
type Anchor struct {
	Events
}

// Events define events occurring within a Anchor.
type Events struct {
	// Fired when a anchor message is received.
	MessageReceived *events.Event
}

// Event defines the information passed when a anchor event fires.
type Event struct {
	Anchor    *jsonmodels.Anchor
	Timestamp time.Time
	MessageID string
}

func chatEventCaller(handler interface{}, params ...interface{}) {
	handler.(func(*Event))(params[0].(*Event))
}

func Encode(a *Payload) *jsonmodels.Anchor {
	return &jsonmodels.Anchor{
		Version:        a.Version,
		ChildTangleID:  base58.Encode(a.ChildTangleID),
		LastStampID:    base58.Encode(a.LastStampID),
		ChildMessageID: base58.Encode(a.ChildMessageID),
		MerkleRoot:     base58.Encode(a.MerkleRoot),
	}
}
