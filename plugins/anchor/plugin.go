package anchor

import (
	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/node"
	"github.com/labstack/echo"
	"go.uber.org/dig"

	"github.com/iotaledger/goshimmer/packages/anchor"
	"github.com/iotaledger/goshimmer/packages/tangle"
)

const (
	// PluginName contains the human-readable name of the plugin.
	PluginName = "anchor"
)

var (
	// Plugin is the "plugin" instance of the chat application.
	Plugin *node.Plugin
	deps   = new(dependencies)
)

func init() {
	Plugin = node.NewPlugin(PluginName, deps, node.Enabled, configure)
	Plugin.Events.Init.Attach(events.NewClosure(func(_ *node.Plugin, container *dig.Container) {
		if err := container.Provide(anchor.NewAnchor); err != nil {
			Plugin.Panic(err)
		}
	}))
}

type dependencies struct {
	dig.In
	Tangle *tangle.Tangle
	Server *echo.Echo
	Anchor *anchor.Anchor
}

func configure(_ *node.Plugin) {
	deps.Tangle.Booker.Events.MessageBooked.Attach(events.NewClosure(onReceiveMessageFromMessageLayer))
	configureWebAPI()
}

func onReceiveMessageFromMessageLayer(messageID tangle.MessageID) {
	var anchorEvent *anchor.Event
	deps.Tangle.Storage.Message(messageID).Consume(func(message *tangle.Message) {
		if message.Payload().Type() != anchor.Type {
			return
		}

		anchorPayload, _, err := anchor.FromBytes(message.Payload().Bytes())
		if err != nil {
			Plugin.LogError(err)
			return
		}

		anchorEvent = &anchor.Event{
			Anchor:    anchor.Encode(anchorPayload),
			Timestamp: message.IssuingTime(),
			MessageID: message.ID().Base58(),
		}
	})

	if anchorEvent == nil {
		return
	}

	deps.Anchor.Events.MessageReceived.Trigger(anchorEvent)
	Plugin.LogInfo(anchorEvent.Anchor)
}
