package mana

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo"

	"github.com/iotaledger/goshimmer/packages/app/jsonmodels"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/mana/manamodels"
	"github.com/iotaledger/hive.go/core/identity"
)

// getNHighestAccessHandler handles a /mana/access/nhighest request.
func getNHighestAccessHandler(c echo.Context) error {
	return nHighestHandler(c, manamodels.AccessMana)
}

// getNHighestConsensusHandler handles a /mana/consensus/nhighest request.
func getNHighestConsensusHandler(c echo.Context) error {
	return nHighestHandler(c, manamodels.ConsensusMana)
}

// nHighestHandler handles the request.
func nHighestHandler(c echo.Context, manaType manamodels.Type) error {
	number, err := strconv.ParseUint(c.QueryParam("number"), 10, 32)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.GetNHighestResponse{Error: err.Error()})
	}
	var manaMap map[identity.ID]int64
	if manaType == manamodels.AccessMana {
		manaMap = deps.Protocol.Engine().ManaTracker.ManaMap()
	} else {
		manaMap = deps.Protocol.Engine().SybilProtection.Weights()
	}
	highestNodes, t, err := manamodels.GetHighestManaIssuers(uint(number), manaMap)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.GetNHighestResponse{Error: err.Error()})
	}
	var res []manamodels.IssuerStr
	for _, n := range highestNodes {
		res = append(res, n.ToIssuerStr())
	}
	return c.JSON(http.StatusOK, jsonmodels.GetNHighestResponse{
		Issuers:   res,
		Timestamp: t.Unix(),
	})
}
