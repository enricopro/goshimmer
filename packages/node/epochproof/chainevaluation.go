package epochproof

import (
	"sync"
	"time"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/mana"
	"github.com/iotaledger/goshimmer/packages/core/tangleold"
	"github.com/iotaledger/hive.go/core/generics/shrinkingmap"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/pkg/errors"
)

type ecVote struct {
	ecRecord  *epoch.ECRecord
	timestamp time.Time
}

type competingECCTracker struct {
	competingECSupporters *shrinkingmap.ShrinkingMap[epoch.Index, map[epoch.EC][]identity.ID]
	latestECVotes         map[identity.ID]*ecVote
	sync.RWMutex
}

func NewCompetingChainTracker() *competingECCTracker {
	return &competingECCTracker{
		competingECSupporters: shrinkingmap.New[epoch.Index, map[epoch.EC][]identity.ID](),
		latestECVotes:         make(map[identity.ID]*ecVote),
	}
}

func (c *competingECCTracker) updateCompetingECSupporters(proof supportersProof, ecRecord *epoch.ECRecord, competingECC epoch.ECChain, confirmedEI epoch.Index) {
	c.Lock()
	defer c.Unlock()

	suppArray := make([]identity.ID, 0)
	for _, suppProof := range proof {
		supporter := identity.NewID(suppProof.issuerPublicKey)
		suppArray = append(suppArray, supporter)
		// updating map for revoking votes
		if vote, exist := c.latestECVotes[supporter]; !exist || suppProof.issuingTime.After(vote.timestamp) {
			c.latestECVotes[supporter] = &ecVote{
				timestamp: suppProof.issuingTime,
				ecRecord:  ecRecord,
			}
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

func (c *competingECCTracker) isCompetingChainHeavier(targetEpoch epoch.Index, forkingPointManaVector mana.BaseManaVector,
	ownValidators map[identity.ID]*tangleold.Block, competingECChain epoch.ECChain) (isCompetingHeavier bool, err error) {
	c.RLock()
	defer c.RUnlock()

	var ownWeight, competingWeight float64
	for validator := range ownValidators {
		validatorWeight, _, err := forkingPointManaVector.GetMana(validator)
		if err != nil {
			return false, errors.Wrap(err, "failed to get validator weight")
		}
		ownWeight += validatorWeight
	}

	supportersEI, exists := c.competingECSupporters.Get(targetEpoch)
	if !exists {
		return false, errors.Errorf("no competing supporters for epoch %d", targetEpoch)
	}
	competingEC := competingECChain[targetEpoch]
	supportersEC, exists := supportersEI[competingECChain[targetEpoch]]
	if !exists {
		return false, errors.Errorf("no competing supporters for epoch %d with EC %s", targetEpoch, competingEC)
	}

	for _, supporterEC := range supportersEC {
		if !isOnChain(c.latestECVotes[supporterEC].ecRecord.ComputeEC(), competingECChain) {
			continue
		}
		supporterWeight, _, err := forkingPointManaVector.GetMana(supporterEC)
		if err != nil {
			return false, errors.Wrap(err, "failed to get supporter weight")
		}
		competingWeight += supporterWeight
	}

	return competingWeight > ownWeight, nil
}

func isOnChain(ec epoch.EC, ecChain epoch.ECChain) bool {
	for _, ecInChain := range ecChain {
		if ecInChain == ec {
			return true
		}
	}
	return false
}
