package inmemory

import (
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/generics/orderedmap"
	"github.com/iotaledger/hive.go/generics/set"
)

type ConflictDAG[ConflictIDType, ResourceIDType comparable] struct {
	conflictsByID         map[ConflictIDType]*Conflict[ConflictIDType, ResourceIDType]
	conflictsByIDMutex    sync.RWMutex
	conflictSetsByID      map[ResourceIDType]*ConflictSet[ConflictIDType, ResourceIDType]
	conflictSetsByIDMutex sync.RWMutex

	sync.RWMutex
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) Conflict(id ConflictIDType) (conflict *Conflict[ConflictIDType, ResourceIDType], exists bool) {
	b.conflictsByIDMutex.RLock()
	defer b.conflictsByIDMutex.RUnlock()

	return b.conflict(id)
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) Conflicts(conflictIDs *set.AdvancedSet[ConflictIDType]) (conflicts *orderedmap.OrderedMap[ConflictIDType, *Conflict[ConflictIDType, ResourceIDType]], err error) {
	b.conflictsByIDMutex.RLock()
	defer b.conflictSetsByIDMutex.RUnlock()

	return b.conflicts(conflictIDs)
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) ConflictSet(id ResourceIDType) (conflictSet *ConflictSet[ConflictIDType, ResourceIDType], exists bool) {
	b.conflictSetsByIDMutex.RLock()
	defer b.conflictSetsByIDMutex.RUnlock()

	return b.conflictSet(id)
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) ConflictSets(conflictSetIDs *set.AdvancedSet[ResourceIDType]) (conflictSets *orderedmap.OrderedMap[ResourceIDType, *ConflictSet[ConflictIDType, ResourceIDType]], err error) {
	b.conflictSetsByIDMutex.RLock()
	defer b.conflictSetsByIDMutex.RUnlock()

	return b.conflictSets(conflictSetIDs)
}

// CreateConflict creates a new Conflict in the ConflictDAG and returns true if the Conflict was new.
func (b *ConflictDAG[ConflictIDType, ResourceIDType]) CreateConflict(id ConflictIDType, parentConflictIDs *set.AdvancedSet[ConflictIDType], conflictingResourceIDs *set.AdvancedSet[ResourceIDType]) (newConflict *Conflict[ConflictIDType, ResourceIDType], err error) {
	newConflict, exists := b.Conflict(id)
	if exists {
		return newConflict, errors.Errorf("conflict with id %v already exists", id)
	}

	parentConflicts, err := b.conflicts(parentConflictIDs)
	if err != nil {
		return nil, errors.Errorf("could not create conflict with id %v: %w", id, err)
	}

	conflictSets, err := b.conflictSets(conflictingResourceIDs, true)
	if err != nil {
		return nil, errors.Errorf("could not create conflict with id %v: %w", id, err)
	}

	newConflict = NewConflict(id, parentConflicts, conflictSets)

	b.conflictsByIDMutex.Lock()
	defer b.conflictSetsByIDMutex.Unlock()

	b.Storage.CachedConflict(id, func(ConflictIDType) (conflict *Conflict[ConflictIDType, ResourceIDType]) {
		conflict = NewConflict(id, parentConflictIDs, set.NewAdvancedSet[ResourceIDType]())

		b.addConflictMembers(conflict, conflictingResourceIDs)
		b.createChildBranchReferences(parentConflictIDs, id)

		if b.anyParentRejected(conflict) || b.anyConflictingBranchConfirmed(conflict) {
			conflict.setInclusionState(Rejected)
		}

		created = true

		return conflict
	}).Release()
	b.RUnlock()

	if created {
		b.Events.ConflictCreated.Trigger(&ConflictCreatedEvent[ConflictIDType, ResourceIDType]{
			ID:                     id,
			ParentConflictIDs:      parentConflictIDs,
			ConflictingResourceIDs: conflictingResourceIDs,
		})
	}

	return created
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) conflict(id ConflictIDType) (conflict *Conflict[ConflictIDType, ResourceIDType], exists bool) {
	conflict, exists = b.conflictsByID[id]
	return conflict, exists
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) conflicts(conflictIDs *set.AdvancedSet[ConflictIDType]) (conflicts *orderedmap.OrderedMap[ConflictIDType, *Conflict[ConflictIDType, ResourceIDType]], err error) {
	conflicts = orderedmap.New[ConflictIDType, *Conflict[ConflictIDType, ResourceIDType]]()
	if err = conflictIDs.ForEach(func(conflictID ConflictIDType) (err error) {
		conflict, conflictExists := b.conflict(conflictID)
		if !conflictExists {
			return errors.Errorf("parent conflict with id %v does not exist", conflictID)
		}

		conflicts.Set(conflictID, conflict)

		return nil
	}); err != nil {
		return nil, err
	}

	return conflicts, nil
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) conflictSet(id ResourceIDType) (conflictSet *ConflictSet[ConflictIDType, ResourceIDType], exists bool) {
	conflictSet, exists = b.conflictSetsByID[id]
	return conflictSet, exists
}

func (b *ConflictDAG[ConflictIDType, ResourceIDType]) conflictSets(conflictSetIDs *set.AdvancedSet[ResourceIDType], createMissing ...bool) (conflictSets *orderedmap.OrderedMap[ResourceIDType, *ConflictSet[ConflictIDType, ResourceIDType]], err error) {
	conflictSets = orderedmap.New[ResourceIDType, *ConflictSet[ConflictIDType, ResourceIDType]]()
	if err = conflictSetIDs.ForEach(func(resourceID ResourceIDType) (err error) {
		conflictSet, conflictSetExists := b.conflictSet(resourceID)
		if !conflictSetExists {
			if len(createMissing) > 0 && createMissing[0] {
				conflictSet = &ConflictSet[ConflictIDType, ResourceIDType]{
					id:      resourceID,
					members: orderedmap.New[ConflictIDType, *Conflict[ConflictIDType, ResourceIDType]](),
				}
				b.conflictSetsByID[resourceID] = conflictSet

				conflictSets.Set(resourceID, conflictSet)

				return nil
			}

			return errors.Errorf("conflict set with id %v does not exist", resourceID)
		}

		conflictSets.Set(resourceID, conflictSet)

		return nil
	}); err != nil {
		return nil, err
	}

	return conflictSets, nil
}
