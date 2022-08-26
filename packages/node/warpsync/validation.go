package warpsync

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/node/p2p"
	wp "github.com/iotaledger/goshimmer/packages/node/warpsync/warpsyncproto"
	"github.com/iotaledger/goshimmer/plugins/epochstorage"
	"github.com/iotaledger/hive.go/core/generics/set"
	"github.com/iotaledger/hive.go/core/identity"
)

type neighborCommitment struct {
	neighbor *p2p.Neighbor
	ecRecord *epoch.ECRecord
}

func (m *Manager) ValidateBackwards(ctx context.Context, start, end epoch.Index, startEC, endPrevEC epoch.EC) (ecChain epoch.ECChain, validPeers *set.AdvancedSet[identity.ID], err error) {
	m.startValidation()
	defer m.stopValidation()

	ecChain = make(epoch.ECChain)
	ecRecordChain := make(epoch.ECRecordChain)
	validPeers = set.NewAdvancedSet(m.p2pManager.AllNeighborsIDs()...)
	activePeers := set.NewAdvancedSet[identity.ID]()
	neighborCommitments := make(map[epoch.Index]map[identity.ID]*neighborCommitment)

	// We do not request the start nor the ending epoch, as we know the beginning (snapshot) and the end (tip received via gossip) of the chain.
	startRange := start + 1
	endRange := end - 1
	for ei := endRange; ei >= startRange; ei-- {
		m.requestEpochCommitment(ei)
	}

	epochToValidate := endRange
	ecChain[start] = startEC
	ecRecordChain[end] = epoch.NewECRecord(end)
	ecRecordChain[end].SetPrevEC(endPrevEC)

	for {
		select {
		case commitment, ok := <-m.commitmentsChan:
			if !ok {
				return nil, nil, nil
			}
			ecRecord := commitment.ecRecord
			peerID := commitment.neighbor.Peer.ID()
			commitmentEI := ecRecord.EI()
			if m.isCommitmentInvalid(peerID, commitmentEI, start, end, validPeers, ecRecord) {

			activePeers.Add(peerID)
				continue
			}

			// If we already validated this epoch, we check if the neighbor is on the target chain.
			if commitmentEI > epochToValidate {
				if ecRecordChain[commitmentEI].ComputeEC() != ecRecord.ComputeEC() {
					m.log.Infof("ignoring commitment and peer that doesn't match already validated chain , peer: %s", peerID.String())
					validPeers.Delete(peerID)
				}
				continue
			}

			// commitmentEI <= epochToValidate
			if neighborCommitments[commitmentEI] == nil {
				neighborCommitments[commitmentEI] = make(map[identity.ID]*neighborCommitment)
			}
			neighborCommitments[commitmentEI][peerID] = commitment

			// We received a commitment out of order, we can evaluate it only later.
			if commitmentEI < epochToValidate {
				continue
			}

			// commitmentEI == epochToValidate
			// Validate commitments collected so far.
			for {
				neighborCommitmentsForEpoch, received := neighborCommitments[epochToValidate]
				// We haven't received commitments for this epoch yet.
				if !received {
					break
				}

				for peerID, epochCommitment := range neighborCommitmentsForEpoch {
					if !validPeers.Has(peerID) {
						continue
					}
					proposedECRecord := epochCommitment.ecRecord
					if ecRecordChain[epochToValidate+1].PrevEC() != proposedECRecord.ComputeEC() {
						m.log.Infof("ignoring commitments, as it doesn't match the previous commitment for the next epoch, peer: %s", peerID.String())
						validPeers.Delete(peerID)
						continue
					}

					// If we already stored the target epoch for the chain, we just keep validating neighbors.
					if _, exists := ecRecordChain[epochToValidate]; exists {
						continue
					}

					ecRecordChain[epochToValidate] = proposedECRecord
					ecChain[epochToValidate] = proposedECRecord.ComputeEC()
				}

				// Stop if we were not able to validate epochToValidate.
				if _, exists := ecRecordChain[epochToValidate]; !exists {
					break
				}

				// We validated the epoch and identified the neighbors that are on the target chain.
				epochToValidate--
				m.log.Debugf("epochs left %d", epochToValidate-start)
			}

			if epochToValidate == start {
				syncedStartPrevEC := ecRecordChain[start+1].PrevEC()
				if startEC != syncedStartPrevEC {
					return nil, nil, errors.Errorf("obtained chain does not match expected starting point EC: expected %s, actual %s", startEC, syncedStartPrevEC)
				}
				m.log.Infof("range %d-%d validated", start, end)
				validPeers = validPeers.Intersect(activePeers)
				return ecChain, validPeers, nil
			}

		case <-ctx.Done():
			return nil, nil, errors.Errorf("cancelled while validating epoch range %d to %d: %s", start, end, ctx.Err())
		}
	}
}

func (m *Manager) isCommitmentInvalid(peerID identity.ID, commitmentEI epoch.Index, startRange epoch.Index, endRange epoch.Index,
	validPeers *set.AdvancedSet[identity.ID], ecRecord *epoch.ECRecord) (invalid bool) {
	m.log.Debugw("read commitment", "EI", commitmentEI, "EC", ecRecord.ComputeEC().Base58())
	// Ignore invalid neighbor.
	if !validPeers.Has(peerID) {
		m.log.Debugw("ignoring invalid neighbor", "ID", peerID, "validPeers", validPeers)
		return true
	}

	// Ignore commitments outside the range.
	if commitmentEI < startRange || commitmentEI > endRange {
		m.log.Debugw("ignoring commitment outside of requested range", "EI", commitmentEI, "peer", peerID)
		return true

	}
	return false
}

func (m *Manager) startValidation() {
	m.validationLock.Lock()
	defer m.validationLock.Unlock()
	m.validationInProgress = true
	m.commitmentsChan = make(chan *neighborCommitment)
	m.commitmentsStopChan = make(chan struct{})
}

func (m *Manager) stopValidation() {
	close(m.commitmentsStopChan)
	m.validationLock.Lock()
	defer m.validationLock.Unlock()
	m.validationInProgress = false
	close(m.commitmentsChan)
}

func (m *Manager) processEpochCommitmentRequestPacket(packetEpochRequest *wp.Packet_EpochCommitmentRequest, nbr *p2p.Neighbor) {
	ei := epoch.Index(packetEpochRequest.EpochCommitmentRequest.GetEI())
	m.log.Debugw("received epoch commitment request", "peer", nbr.Peer.ID(), "EI", ei)

	ecRecord, exists := epochstorage.GetEpochCommittment(ei)
	if !exists {
		return
	}

	m.sendEpochCommitmentMessage(ei, ecRecord.ECR(), ecRecord.PrevEC(), nbr.ID())

	m.log.Debugw("sent epoch commitment", "peer", nbr.Peer.ID(), "EI", ei, "EC", ecRecord.ComputeEC().Base58())
}

func (m *Manager) processEpochCommitmentPacket(packetEpochCommitment *wp.Packet_EpochCommitment, nbr *p2p.Neighbor) {
	m.validationLock.RLock()
	defer m.validationLock.RUnlock()

	if !m.validationInProgress {
		return
	}

	ei := epoch.Index(packetEpochCommitment.EpochCommitment.GetEI())
	ecr := epoch.NewMerkleRoot(packetEpochCommitment.EpochCommitment.GetECR())
	prevEC := epoch.NewMerkleRoot(packetEpochCommitment.EpochCommitment.GetPrevEC())

	ecRecord := epoch.NewECRecord(ei)
	ecRecord.SetECR(ecr)
	ecRecord.SetPrevEC(prevEC)

	m.log.Debugw("received epoch commitment", "peer", nbr.Peer.ID(), "EI", ei, "EC", ecRecord.ComputeEC().Base58())

	select {
	case <-m.commitmentsStopChan:
		return
	case m.commitmentsChan <- &neighborCommitment{
		neighbor: nbr,
		ecRecord: ecRecord,
	}:
	}
}
