package anchor

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"

	"github.com/labstack/echo"
	"github.com/wilfreddenton/merkle"
	"golang.org/x/crypto/blake2b"

	"github.com/iotaledger/goshimmer/packages/anchor"
	"github.com/iotaledger/goshimmer/packages/jsonmodels"
	"github.com/iotaledger/goshimmer/packages/tangle"
)

var (
	lastAnchor   map[string]tangle.MessageID
	storage      map[string]tangle.MessageID
	storageMutex sync.RWMutex
)

func configureWebAPI() {
	deps.Server.POST("anchor", SendAnchorMessage)
	deps.Server.GET("anchor:ID", MessageIDFromChildMessageID)
	deps.Server.POST("proofofinclusion/verify", Verify)
	lastAnchor = make(map[string]tangle.MessageID)
	storage = make(map[string]tangle.MessageID)
}

func Verify(c echo.Context) error {
	req := &jsonmodels.Proof{}
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.NewErrorResponse(err))
	}

	valid, err := verify(req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.NewErrorResponse(err))
	}

	return c.JSON(http.StatusOK, &jsonmodels.Verify{
		Valid: valid,
	})
}

func verify(proof *jsonmodels.Proof) (v bool, err error) {
	h, err := blake2b.New256(nil)
	if err != nil {
		return
	}

	t, err := hex.DecodeString(proof.MessageID)
	if err != nil {
		return
	}
	leave := merkle.LeafHash(t, h)

	r := merkleRoot(proof.StampID)
	if r == nil {
		err = fmt.Errorf("anchor not found")
		return
	}
	root, err := hex.DecodeString(r.MerkleRoot)
	if err != nil {
		return
	}

	p := proof.DecodePath()

	return merkle.Prove(leave, root, p, h), nil
}

func merkleRoot(stampID string) *jsonmodels.AnchorResponse {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	if messageID, ok := storage[stampID]; ok {
		var merkleRoot string
		deps.Tangle.Storage.Message(messageID).Consume(func(message *tangle.Message) {
			merkleRoot = hex.EncodeToString(message.Payload().(*anchor.Payload).MerkleRoot)
		})
		return &jsonmodels.AnchorResponse{
			MessageID:  messageID.Base58(),
			MerkleRoot: merkleRoot,
		}
	}
	return nil
}

func MessageIDFromChildMessageID(c echo.Context) error {
	cmID := c.Param("ID")

	resp := merkleRoot(cmID)
	if resp == nil {
		return c.JSON(http.StatusNotFound, jsonmodels.AnchorResponse{Error: "AnchorID not found"})
	}
	return c.JSON(http.StatusOK, resp)
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
	key := hex.EncodeToString(anchorPayload.ChildTangleID)
	lastStampID := lastAnchor[key]
	anchorPayload.LastStampID = lastStampID[:]
	Plugin.LogInfo(anchorPayload)

	msg, err := deps.Tangle.IssuePayload(anchorPayload)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.AnchorResponse{Error: err.Error()})
	}
	lastAnchor[key] = msg.ID()
	storageMutex.Lock()
	defer storageMutex.Unlock()
	storage[hex.EncodeToString(anchorPayload.ChildMessageID)] = msg.ID()

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
