package epochproof

import (
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/mana"
	"github.com/iotaledger/hive.go/core/generics/shrinkingmap"
	"github.com/iotaledger/hive.go/core/identity"
)

type competingECCTracker struct {
	competingECSupporters *shrinkingmap.ShrinkingMap[epoch.Index, map[epoch.EC][]identity.ID]
	latestECVotes         map[identity.ID]*ecVote
}

func NewCompetingChainTracker() *competingECCTracker {
	return &competingECCTracker{
		competingECSupporters: shrinkingmap.New[epoch.Index, map[epoch.EC][]identity.ID](),
		latestECVotes:         make(map[identity.ID]*ecVote),
	}
}

func (c *competingECCTracker) updateCompetingECSupporters(proof supportersProof, ecRecord *epoch.ECRecord, competingECC epoch.ECChain, confirmedEI epoch.Index) {
	suppArray := make([]identity.ID, 0)

	for _, suppProof := range proof {
		supporter := identity.NewID(suppProof.issuerPublicKey)
		suppArray = append(suppArray, supporter)
		// updating map for revoking votes
		if vote, exist := c.latestECVotes[supporter]; !exist || suppProof.issuingTime.After(vote.timestamp) {
			c.latestECVotes[supporter] = &ecVote{timestamp: suppProof.issuingTime, ecRecord: ecRecord}
		}
	}
	// propagate the voters for all ec in the competing ECC
	for ei := confirmedEI; ei <= ecRecord.EI(); ei++ {
		ec := competingECC[ei]
		epochSupporters, exists := c.competingECSupporters.Get(ei)
		if !exists {
			epochSupporters = make(map[epoch.EC][]identity.ID, 0)
			c.competingECSupporters.Set(ei, epochSupporters)
		}
		ecSupporters, exists := epochSupporters[ec]
		if !exists {
			ecSupporters = make([]identity.ID, 0)
		}
		ecSupporters = append(ecSupporters, suppArray...)
		epochSupporters[ec] = ecSupporters
	}
}

func (c *competingECCTracker) selectHeaviestChain(vector mana.BaseManaVector) {

}
