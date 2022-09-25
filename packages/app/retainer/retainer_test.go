package retainer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/iotaledger/hive.go/core/serix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/protocol/database"
	"github.com/iotaledger/goshimmer/packages/protocol/engine"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/eviction"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/booker/markers"
	"github.com/iotaledger/goshimmer/packages/protocol/ledger/utxo"
	"github.com/iotaledger/goshimmer/packages/protocol/models"
)

func TestRetainer_BlockMetadata_Serialization(t *testing.T) {
	meta := createBlockMetadata()

	serializedBytes, err := meta.Bytes()
	assert.NoError(t, err)

	metaDeserialized := newBlockMetadata(nil)
	decodedBytes, err := metaDeserialized.FromBytes(serializedBytes)
	assert.NoError(t, err)
	assert.Equal(t, len(serializedBytes), decodedBytes)

	validateDeserialized(t, meta, metaDeserialized)
}

func TestRetainer_BlockMetadata_JSON(t *testing.T) {
	meta := createBlockMetadata()
	out, err := serix.DefaultAPI.JSONEncode(context.Background(), meta.M)
	require.NoError(t, err)
	printPrettyJSON(t, out)

	metaDeserialized := newBlockMetadata(nil)
	err = serix.DefaultAPI.JSONDecode(context.Background(), out, &metaDeserialized.M)
	assert.NoError(t, err)
	validateDeserialized(t, meta, metaDeserialized)
}

func TestRetainer_BlockMetadata_NonEvicted(t *testing.T) {
	tf := engine.NewTestFramework(t)
	retainer := NewRetainer(tf.Engine, database.NewManager())
	b := tf.Tangle.CreateBlock("A")
	tf.Tangle.IssueBlocks("A").WaitUntilAllTasksProcessed()
	block, exists := tf.CongestionControl.Block(b.ID())
	assert.True(t, exists)
	meta, exists := retainer.BlockMetadata(block.ID())
	assert.True(t, exists)

	assert.Equal(t, meta.M.Missing, block.IsMissing())
	assert.Equal(t, meta.M.Solid, block.IsSolid())
	assert.Equal(t, meta.M.Invalid, block.IsInvalid())
	assert.Equal(t, meta.M.Orphaned, block.IsOrphaned())
	assert.Equal(t, meta.M.OrphanedBlocksInPastCone, block.OrphanedBlocksInPastCone())
	assert.Equal(t, meta.M.StrongChildren, blocksToBlockIDs(block.StrongChildren()))
	assert.Equal(t, meta.M.WeakChildren, blocksToBlockIDs(block.WeakChildren()))
	assert.Equal(t, meta.M.LikedInsteadChildren, blocksToBlockIDs(block.LikedInsteadChildren()))
	assert.Equal(t, meta.M.Booked, block.IsBooked())
	assert.EqualValues(t, meta.M.StructureDetails.IsPastMarker, block.StructureDetails().IsPastMarker())
	assert.EqualValues(t, meta.M.StructureDetails.Rank, block.StructureDetails().Rank())
	assert.EqualValues(t, meta.M.StructureDetails.PastMarkerGap, block.StructureDetails().PastMarkerGap())

	pastMarkers := markers.NewMarkers()
	for sequenceID, index := range meta.M.StructureDetails.PastMarkers {
		pastMarkers.Set(sequenceID, index)
	}
	assert.EqualValues(t, pastMarkers, block.StructureDetails().PastMarkers())
	assert.Equal(t, meta.M.AddedConflictIDs, block.AddedConflictIDs())
	assert.Equal(t, meta.M.SubtractedConflictIDs, block.SubtractedConflictIDs())
	assert.Equal(t, meta.M.ConflictIDs, tf.Engine.Tangle.BlockConflicts(block.Block.Block))

	assert.Equal(t, meta.M.Tracked, true)
	assert.Equal(t, meta.M.SubjectivelyInvalid, block.IsSubjectivelyInvalid())
	assert.Equal(t, meta.M.Scheduled, block.IsScheduled())
	assert.Equal(t, meta.M.Skipped, block.IsSkipped())
	assert.Equal(t, meta.M.Dropped, block.IsDropped())
	assert.Equal(t, meta.M.Accepted, false)
}

func TestRetainer_BlockMetadata_Evicted(t *testing.T) {
	epoch.GenesisTime = time.Now().Add(-5 * time.Minute).Unix()
	evictionManager := eviction.NewManager[models.BlockID]()
	tf := engine.NewTestFramework(t, engine.WithEvictionManager(evictionManager))
	retainer := NewRetainer(tf.Engine, database.NewManager())
	b := tf.Tangle.CreateBlock("A")
	tf.Tangle.IssueBlocks("A").WaitUntilAllTasksProcessed()
	block, exists := tf.CongestionControl.Block(b.ID())
	assert.True(t, exists)
	evictionManager.EvictUntil(b.ID().EpochIndex+1, nil)
	tf.Tangle.BlockDAGTestFramework.WaitUntilAllTasksProcessed()

	meta, exists := retainer.BlockMetadata(block.ID())
	assert.True(t, exists)

	assert.Equal(t, meta.M.Missing, block.IsMissing())
	assert.Equal(t, meta.M.Solid, block.IsSolid())
	assert.Equal(t, meta.M.Invalid, block.IsInvalid())
	assert.Equal(t, meta.M.Orphaned, block.IsOrphaned())
	assert.Equal(t, meta.M.OrphanedBlocksInPastCone, block.OrphanedBlocksInPastCone())
	assert.Equal(t, meta.M.StrongChildren, blocksToBlockIDs(block.StrongChildren()))
	assert.Equal(t, meta.M.WeakChildren, blocksToBlockIDs(block.WeakChildren()))
	assert.Equal(t, meta.M.LikedInsteadChildren, blocksToBlockIDs(block.LikedInsteadChildren()))
	assert.Equal(t, meta.M.Booked, block.IsBooked())
	assert.EqualValues(t, meta.M.StructureDetails.IsPastMarker, block.StructureDetails().IsPastMarker())
	assert.EqualValues(t, meta.M.StructureDetails.Rank, block.StructureDetails().Rank())
	assert.EqualValues(t, meta.M.StructureDetails.PastMarkerGap, block.StructureDetails().PastMarkerGap())

	pastMarkers := markers.NewMarkers()
	for sequenceID, index := range meta.M.StructureDetails.PastMarkers {
		pastMarkers.Set(sequenceID, index)
	}
	assert.EqualValues(t, pastMarkers, block.StructureDetails().PastMarkers())
	assert.Equal(t, meta.M.AddedConflictIDs, block.AddedConflictIDs())
	assert.Equal(t, meta.M.SubtractedConflictIDs, block.SubtractedConflictIDs())
	assert.Equal(t, meta.M.ConflictIDs, tf.Engine.Tangle.BlockConflicts(block.Block.Block))
	assert.Equal(t, meta.M.Tracked, true)
	assert.Equal(t, meta.M.SubjectivelyInvalid, block.IsSubjectivelyInvalid())
	assert.Equal(t, meta.M.Scheduled, block.IsScheduled())
	assert.Equal(t, meta.M.Skipped, block.IsSkipped())
	assert.Equal(t, meta.M.Dropped, block.IsDropped())
	assert.Equal(t, meta.M.Accepted, false)
}

func validateDeserialized(t *testing.T, meta *BlockMetadata, metaDeserialized *BlockMetadata) {
	assert.Equal(t, meta.M.Missing, metaDeserialized.M.Missing)
	assert.Equal(t, meta.M.Solid, metaDeserialized.M.Solid)
	assert.Equal(t, meta.M.Invalid, metaDeserialized.M.Invalid)
	assert.Equal(t, meta.M.Orphaned, metaDeserialized.M.Orphaned)
	assert.Equal(t, meta.M.OrphanedBlocksInPastCone, metaDeserialized.M.OrphanedBlocksInPastCone)
	assert.Equal(t, meta.M.StrongChildren, metaDeserialized.M.StrongChildren)
	assert.Equal(t, meta.M.WeakChildren, metaDeserialized.M.WeakChildren)
	assert.Equal(t, meta.M.LikedInsteadChildren, metaDeserialized.M.LikedInsteadChildren)
	assert.Equal(t, meta.M.SolidTime.Unix(), metaDeserialized.M.SolidTime.Unix())
	assert.Equal(t, meta.M.Booked, metaDeserialized.M.Booked)
	assert.EqualValues(t, meta.M.StructureDetails, metaDeserialized.M.StructureDetails)
	// TODO: implement JSON serialization for AdvancedSet or OrderedMap
	// assert.Equal(t, meta.M.AddedConflictIDs, metaDeserialized.M.AddedConflictIDs)
	// assert.Equal(t, meta.M.SubtractedConflictIDs, metaDeserialized.M.SubtractedConflictIDs)
	// assert.Equal(t, meta.M.ConflictIDs, metaDeserialized.M.ConflictIDs)
	assert.Equal(t, meta.M.BookedTime.Unix(), metaDeserialized.M.BookedTime.Unix())
	assert.Equal(t, meta.M.Tracked, metaDeserialized.M.Tracked)
	assert.Equal(t, meta.M.SubjectivelyInvalid, metaDeserialized.M.SubjectivelyInvalid)
	assert.Equal(t, meta.M.TrackedTime.Unix(), metaDeserialized.M.TrackedTime.Unix())
	assert.Equal(t, meta.M.Scheduled, metaDeserialized.M.Scheduled)
	assert.Equal(t, meta.M.Skipped, metaDeserialized.M.Skipped)
	assert.Equal(t, meta.M.Dropped, metaDeserialized.M.Dropped)
	assert.Equal(t, meta.M.SchedulerTime.Unix(), metaDeserialized.M.SchedulerTime.Unix())
	assert.Equal(t, meta.M.Accepted, metaDeserialized.M.Accepted)
	assert.Equal(t, meta.M.AcceptedTime.Unix(), metaDeserialized.M.AcceptedTime.Unix())
}

func createBlockMetadata() *BlockMetadata {
	var blockID0, blockID1, blockID2 models.BlockID
	_ = blockID0.FromRandomness()
	_ = blockID1.FromRandomness()
	_ = blockID2.FromRandomness()

	meta := newBlockMetadata(nil)
	meta.SetID(blockID0)
	meta.M.Missing = false
	meta.M.Solid = true
	meta.M.Invalid = false
	meta.M.Orphaned = true
	meta.M.OrphanedBlocksInPastCone = make(models.BlockIDs)
	meta.M.OrphanedBlocksInPastCone.Add(blockID1)
	meta.M.StrongChildren = make(models.BlockIDs)
	meta.M.StrongChildren.Add(blockID2)
	meta.M.WeakChildren = make(models.BlockIDs)
	meta.M.WeakChildren.Add(blockID2)
	meta.M.LikedInsteadChildren = make(models.BlockIDs)
	meta.M.LikedInsteadChildren.Add(blockID2)
	meta.M.SolidTime = time.Now()

	meta.M.Booked = true
	meta.M.StructureDetails = &structureDetails{
		Rank:          4,
		PastMarkerGap: 3,
		IsPastMarker:  true,
		PastMarkers:   map[markers.SequenceID]markers.Index{markers.SequenceID(5): markers.Index(1)},
	}
	meta.M.AddedConflictIDs = utxo.NewTransactionIDs(utxo.EmptyTransactionID, utxo.NewTransactionID([]byte("test")))
	meta.M.SubtractedConflictIDs = utxo.NewTransactionIDs(utxo.NewTransactionID([]byte("test1")), utxo.NewTransactionID([]byte("test2")))
	meta.M.ConflictIDs = utxo.NewTransactionIDs(utxo.NewTransactionID([]byte("test1")), utxo.NewTransactionID([]byte("test2")))
	meta.M.BookedTime = time.Now()
	meta.M.Tracked = true
	meta.M.SubjectivelyInvalid = true
	meta.M.TrackedTime = time.Now()
	meta.M.Scheduled = true
	meta.M.Skipped = false
	meta.M.Dropped = false
	meta.M.SchedulerTime = time.Now()
	meta.M.Accepted = true
	meta.M.AcceptedTime = time.Now()
	return meta
}
func printPrettyJSON(t *testing.T, b []byte) {
	var prettyJSON bytes.Buffer
	require.NoError(t, json.Indent(&prettyJSON, b, "", "    "))
	fmt.Println(prettyJSON.String())
}
