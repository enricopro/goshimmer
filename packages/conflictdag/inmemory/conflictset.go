package inmemory

import (
	"sync"

	"github.com/iotaledger/hive.go/generics/orderedmap"
)

type ConflictSet[ConflictID, ConflictSetID comparable] struct {
	id      ConflictSetID
	members *orderedmap.OrderedMap[ConflictID, *Conflict[ConflictID, ConflictSetID]]

	sync.RWMutex
}
