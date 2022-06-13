package inmemory

import (
	"sync"

	"github.com/iotaledger/hive.go/generics/orderedmap"
)

type Conflict[ConflictID, ConflictingResourceID comparable] struct {
	// id contains the identifier of the conflict.
	id ConflictID

	// parents contains the parent BranchIDs that this Conflict depends on.
	parents *orderedmap.OrderedMap[ConflictID, *Conflict[ConflictID, ConflictingResourceID]]

	// children contains the causal children of the Conflict.
	children *orderedmap.OrderedMap[ConflictID, *Conflict[ConflictID, ConflictingResourceID]]

	// conflictIDs contains the identifiers of the conflicts that this Conflict is part of.
	conflictSets *orderedmap.OrderedMap[ConflictingResourceID, *ConflictSet[ConflictID, ConflictingResourceID]]

	// inclusionState contains the InclusionState of the Conflict.
	inclusionState InclusionState

	sync.RWMutex
}

func NewConflict[ConflictID comparable, ConflictingResourceID comparable](id ConflictID, parents *orderedmap.OrderedMap[ConflictID, *Conflict[ConflictID, ConflictingResourceID]], conflictSets *orderedmap.OrderedMap[ConflictingResourceID, *ConflictSet[ConflictID, ConflictingResourceID]]) (newConflict *Conflict[ConflictID, ConflictingResourceID]) {
	newConflict = &Conflict[ConflictID, ConflictingResourceID]{
		id:           id,
		parents:      parents,
		conflictSets: conflictSets,
	}

	parents.ForEach(func(parentConflictID ConflictID, parentConflict *Conflict[ConflictID, ConflictingResourceID]) (success bool) {
		parentConflict.Lock()
		defer parentConflict.Unlock()

		if parentConflict.inclusionState == Rejected {

		}

		parentConflict.children.Set(id, newConflict)

		return true
	})

	conflictSets.ForEach(func(resourceID ConflictingResourceID, conflictSet *ConflictSet[ConflictID, ConflictingResourceID]) (success bool) {
		conflictSet.Lock()
		defer conflictSet.Unlock()

		conflictSet.members.Set(id, newConflict)

		return true
	})

	return newConflict
}
