package epochproof

import (
	"github.com/cockroachdb/errors"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/core/mana"
	"github.com/iotaledger/goshimmer/plugins/blocklayer"
)

// manaVectorForEpoch returns the mana vector at the specified epoch.
func (m *Manager) manaVectorForEpoch(targetEpoch epoch.Index) (manaVector mana.BaseManaVector, err error) {
	m.notarizationManager.RLock()
	cei, err := m.notarizationManager.LatestConfirmedEpochIndex()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest confirmed epoch index")
	}
	if cei > targetEpoch {
		return nil, errors.Errorf("requested epoch %d is below confirmed epoch %d", targetEpoch, cei)

	}

	manaVector = blocklayer.ConfirmedCManaVector.Clone()
	epochDiffs, err := m.notarizationManager.GetEpochDiffs(cei+1, targetEpoch)
	m.notarizationManager.RUnlock()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get epoch diffs")
	}

	for ei := cei + 1; ei <= targetEpoch; ei++ {
		manaVector.BookEpoch(epochDiffs[ei].Created(), epochDiffs[ei].Spent(), false)
	}

	return
}
