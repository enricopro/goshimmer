package anchor

import (
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo"

	"github.com/iotaledger/goshimmer/packages/anchor"
	"github.com/iotaledger/goshimmer/packages/jsonmodels"
)

func configureWebAPI() {
	deps.Server.POST("anchor", SendAnchorMessage)
}

// SendAnchorMessage sends an anchor message.
func SendAnchorMessage(c echo.Context) error {
	req := &jsonmodels.Anchor{}
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.NewErrorResponse(err))
	}

	anchorPayload, err := parseAnchor(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.AnchorResponse{Error: err.Error()})
	}
	msg, err := deps.Tangle.IssuePayload(anchorPayload)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.AnchorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, jsonmodels.AnchorResponse{MessageID: msg.ID().Base58()})
}

func parseAnchor(aj *jsonmodels.Anchor) (a *anchor.Payload, err error) {
	childTangleID, err := hex.DecodeString(aj.ChildTangleID)
	if err != nil {
		return nil, err
	}

	lastStampID, err := hex.DecodeString(aj.LastStampID)
	if err != nil {
		return nil, err
	}

	childMessageID, err := hex.DecodeString(aj.ChildMessageID)
	if err != nil {
		return nil, err
	}

	merkleRoot, err := hex.DecodeString(aj.MerkleRoot)
	if err != nil {
		return nil, err
	}

	return anchor.NewPayload(aj.Version, childTangleID, lastStampID, childMessageID, merkleRoot), nil
}
