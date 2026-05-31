package skiplist

// SkipList is a concurrent data structure that implements a skip list.
// It is a probabilistic data structure that allows for fast search, insert,
// and delete operations.
// Locks are acquired in the same order, in this case from the lowest level to the highest level.
import (
	"cmp"
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/RICE-COMP318-FALL24/owldb-p1group12/pair"
)

const MAX_LEVEL = 32

// SkipList is the concrete implementation
type SkipList[K cmp.Ordered, V any] struct {
	head      *Node[K, V]
	timeStamp atomic.Int64
	tail      *Node[K, V]
}

type Node[K cmp.Ordered, V any] struct {
	mtx sync.Mutex

	key      K
	value    V
	topLevel int

	// whether the node has been removed from the list. defaults to false.
	marked      atomic.Bool
	fullyLinked atomic.Bool
	next        []atomic.Pointer[Node[K, V]]
}

func New[K cmp.Ordered, V any](minKey K, maxKey K) *SkipList[K, V] {
	var dummyHead Node[K, V] = Node[K, V]{
		key: minKey,
	}
	var dummyTail Node[K, V] = Node[K, V]{
		key: maxKey,
	}

	dummyHead.next = make([]atomic.Pointer[Node[K, V]], MAX_LEVEL)
	dummyTail.next = make([]atomic.Pointer[Node[K, V]], MAX_LEVEL)

	for i := 0; i < MAX_LEVEL; i++ {
		dummyHead.next[i] = atomic.Pointer[Node[K, V]]{}
		dummyHead.next[i].Store(&dummyTail)
	}
	dummyHead.fullyLinked.Store(true)
	slog.Debug("sl: Creating SkipList",
		slog.Any("dummyHeadKey", dummyHead.key),
		slog.Any("dummyTailKey", dummyTail.key))
	return &SkipList[K, V]{
		head:      &dummyHead,
		timeStamp: atomic.Int64{},
		tail:      &dummyTail,
	}
}

// Helper method to find the predecessor and successor nodes of the key.
// Returns the level at which the key was found, the predecessors, and the successors
func (sl *SkipList[K, V]) find(key K) (int, []*Node[K, V], []*Node[K, V]) {
	slog.Debug("sl: Finding key: ", slog.Any("key", key))
	foundLevel := -1
	pred := sl.head
	level := MAX_LEVEL - 1
	preds := make([]*Node[K, V], MAX_LEVEL)
	succs := make([]*Node[K, V], MAX_LEVEL)
	for level >= 0 {
		curr := pred.next[level].Load()
		for key > curr.key {
			pred = curr
			curr = pred.next[level].Load()
		}
		if foundLevel == -1 && key == curr.key {
			foundLevel = level
		}
		preds[level] = pred
		succs[level] = curr
		level = level - 1
	}
	slog.Debug("sl: Found key: ", slog.Any("key", key), " at level: ", foundLevel)
	return foundLevel, preds, succs
}

// Find retrieves a value associated with the key and whether it was fouund
// If it was not found, the value is NOT defined and should NOT be checked
func (sl *SkipList[K, V]) Find(key K) (foundValue V, found bool) {
	levelFound, _, succs := sl.find(key)

	if levelFound == -1 {
		var zeroValue V
		slog.Debug("sl: Key not found: ", slog.Any("key", key))
		return zeroValue, false
	}

	foundNode := succs[levelFound]
	slog.Debug("sl: Found node: ", slog.Any("foundNode", foundNode))
	return foundNode.value, foundNode.fullyLinked.Load() && !foundNode.marked.Load()

}

func randomLevel() int {
	level := 1
	// Generate a random level between 1 and MAX_LEVEL
	// with a 50% chance of incrementing the level
	for level < MAX_LEVEL && rand.Intn(2) == 0 {
		level++
	}
	return level
}

// Upsert inserts or updates a value associated with the key.
// Check func is there to add flexibility to the Upsert operation; it allows the caller to define
// the behavior of the Upsert operation and which value to use.
func (sl *SkipList[K, V]) Upsert(key K, check func(key K, currValue V, exists bool) (newValue V, err error)) (bool, error) {
	// Pick random top level
	topLevel := randomLevel()
	// Keep trying to insert until success/failure
	for {
		// Check if key is already in list
		levelFound, preds, succs := sl.find(key)
		if levelFound != -1 {
			// Key is already in list
			slog.Debug("sl: Key already in list: ", slog.Any("key", key))
			foundNode := succs[levelFound]
			if !foundNode.marked.Load() {
				slog.Debug("sl: Upsert found node: ", slog.Any("foundNode", foundNode))
				// Node is being added, wait for other insert to finish
				for !foundNode.fullyLinked.Load() {
				}
				// Lock node to attempt Update
				foundNode.mtx.Lock()
				slog.Debug("sl: Upsert locked node: ", slog.Any("foundNode", foundNode))
				// Check if node is still in list
				if foundNode.marked.Load() {
					slog.Debug("sl: Upsert aborted as node being removed, unlocking: ", slog.Any("foundNode", foundNode))
					foundNode.mtx.Unlock()
					// Node has been removed, try again
					continue
				}
				// Call check function
				newValue, err := check(key, foundNode.value, true)
				if err != nil {
					slog.Debug("sl: Upsert check failed; unlocking node: ", slog.Any("foundNode", foundNode))
					foundNode.mtx.Unlock()
					return false, nil
				}
				// Update value once fully linked
				slog.Debug("sl: Upsert updating and unlocking node node: ", slog.Any("foundNode", foundNode), " with new value: ", newValue)
				foundNode.value = newValue
				foundNode.mtx.Unlock()
				return true, nil
			}
			// Found node is being removed, try again
			continue
		}

		// If not, lock all predecessors
		highestLocked := -1
		valid := true
		level := 0
		// Lock all predecessors, with set of already locked nodes to not deadlock
		locked := make(map[*Node[K, V]]struct{}, topLevel+1)
		for valid && level <= topLevel {
			slog.Debug("sl: Upsert locking pred to insert: ", slog.Any("pred", preds[level]))
			if _, ok := locked[preds[level]]; !ok {
				slog.Debug("sl:    .. locking pred", slog.Any("pred", preds[level]))
				preds[level].mtx.Lock()
				highestLocked = level
				locked[preds[level]] = struct{}{}
			}
			// Check if pred/succ are still valid
			unmarked := !preds[level].marked.Load() && !succs[level].marked.Load()
			connected := preds[level].next[level].Load() == succs[level]
			valid = unmarked && connected
			level = level + 1
		}
		// If not valid, unlock and try again
		if !valid {
			slog.Debug("sl: Upsert invalid pred/succ, unlocking and trying again")
			// Predecessors or successors changed,
			// unlock and try again
			level = highestLocked
			for level >= 0 {
				if _, ok := locked[preds[level]]; ok {
					slog.Debug("sl:    .. unlocking pred", slog.Any("pred", preds[level]))
					preds[level].mtx.Unlock()
					delete(locked, preds[level])

				}
				level = level - 1
			}
			continue
		}
		// If all locked and still valid, insert node with zero value
		var zeroValue V

		newValue, err := check(key, zeroValue, false)
		slog.Debug("sl: Upsert about to try to insert node: ", slog.Any("key", key), slog.Any("value", zeroValue))

		if err != nil {
			// Error in check function; unlock and abort
			slog.Debug("sl: Upsert check failed, unlocking and aborting")
			level = highestLocked
			for level >= 0 {
				if _, ok := locked[preds[level]]; ok {
					slog.Debug("sl:    .. unlocking pred", slog.Any("pred", preds[level]))
					preds[level].mtx.Unlock()
					delete(locked, preds[level])

				}
				level = level - 1
			}
			return false, err
		}
		// Increment modification timestamp as new node commited to being added
		sl.timeStamp.Add(1)

		// Create new node
		newNode := Node[K, V]{
			key:      key,
			value:    newValue,
			topLevel: topLevel,
			next:     make([]atomic.Pointer[Node[K, V]], topLevel+1),
		}

		slog.Debug("sl: Upsert created new node: ", slog.Any("key", newNode.key), slog.Any("value", newNode.value), slog.Any("topLevel", newNode.topLevel))
		// Set next pointers
		for level := 0; level <= topLevel; level++ {
			newNode.next[level].Store(succs[level])
		}
		// Link new node
		for level := 0; level <= topLevel; level++ {
			preds[level].next[level].Store(&newNode)
		}

		// Node has been fully added
		slog.Debug("sl: Upsert inserted and fully linked node: ", slog.Any("key", newNode.key), slog.Any("value", newNode.value), slog.Any("topLevel", newNode.topLevel))

		newNode.fullyLinked.Store(true)

		// Unlock all predecessors
		slog.Debug("sl: Upsert unlocking all preds...")
		level = highestLocked
		for level >= 0 {
			if _, ok := locked[preds[level]]; ok {
				slog.Debug("sl:    .. unlocking pred", slog.Any("pred", &preds[level]))
				preds[level].mtx.Unlock()
				delete(locked, preds[level])

			}
			level = level - 1
		}
		return true, nil

	}

}

// Remove deletes a value associated with the key.
func (sl *SkipList[K, V]) Remove(key K) (removedValue V, removed bool) {
	nilNode := Node[K, V]{}
	victim := &nilNode // Victim node to remove
	isMarked := false  // Have we already marked the victim?
	topLevel := -1     // Top level of victim node
	// Keep trying to remove until success/failure
	for {
		slog.Debug("sl: Remove: entering loop to remove key: ", slog.Any("key", key))
		// Find victim (or fail), lock and mark it on first iteration
		levelFound, preds, succs := sl.find(key)
		if levelFound != -1 {
			victim = succs[levelFound]
		}
		if !isMarked {
			// First time through
			if levelFound == -1 {
				// No matching node found
				slog.Debug("sl: Remove: key not found: ", slog.Any("key", key))
				return nilNode.value, false
			}
			if !victim.fullyLinked.Load() {
				// Victim not yet inserted
				slog.Debug("sl: Remove: victim not fully linked: ", slog.Any("victim", victim))
				return nilNode.value, false
			}
			if victim.marked.Load() {
				// Victim already being removed
				slog.Debug("sl: Remove: victim already marked: ", slog.Any("victim", victim))
				return nilNode.value, false
			}
			if victim.topLevel != levelFound {
				// Wasn't fullyLinked when found
				slog.Debug("sl: Remove: victim not fully linked: ", slog.Any("victim", victim))
				return nilNode.value, false
			}
			topLevel = victim.topLevel
			victim.mtx.Lock()
			if victim.marked.Load() {
				// Another remove call beat us
				slog.Debug("sl: Remove: victim already marked: ", slog.Any("victim", victim))
				victim.mtx.Unlock()
				return nilNode.value, false
			}
			slog.Debug("sl: Remove: marked victim and commiting to removal: ", slog.Any("victim", victim))
			victim.marked.Store(true)

			// Increment modification timestamp as node commited to being removed
			sl.timeStamp.Add(1)
			isMarked = true
		}

		// Victim is locked and marked
		// Lock all predecessors and validate
		highestLocked := -1
		level := 0
		valid := true

		// Lock all predecessors, with set of locked nodes to not deadlock as in upsert
		locked := make(map[*Node[K, V]]struct{}, topLevel+1)
		for valid && (level <= topLevel) {
			pred := preds[level]
			slog.Debug("sl: Remove: locking pred: ", slog.Any("pred", preds[level]))
			if _, ok := locked[preds[level]]; !ok {
				preds[level].mtx.Lock()
				highestLocked = level
				locked[preds[level]] = struct{}{}
			}
			successor := pred.next[level].Load() == victim
			valid = !pred.marked.Load() && successor
			level = level + 1
		}
		// Predecessors changed, try again
		// victim remains locked and marked
		if !valid {
			slog.Debug("sl: Remove: found invalid pred, unlocking and trying again")
			level = highestLocked
			for level >= 0 {
				if _, ok := locked[preds[level]]; ok {
					preds[level].mtx.Unlock()
					delete(locked, preds[level])
				}
				level = level - 1
			}
			continue
		}
		// All preds are locked and valid, unlink / remove
		slog.Debug("sl: Remove: removed and unlinking victim: ", slog.Any("victim", victim))
		level = topLevel
		for level >= 0 {
			preds[level].next[level].Store(victim.next[level].Load())
			level = level - 1
		}
		// Unlock
		victim.mtx.Unlock()
		level = highestLocked
		for level >= 0 {
			if _, ok := locked[preds[level]]; ok {
				preds[level].mtx.Unlock()
				delete(locked, preds[level])
			}
			level = level - 1
		}
		return victim.value, true
	}
}

// Query retrieves a range of key-value pairs between start and end.
// It does this by finding the lower bound, then traversing the level 0 list
// until the upper bound is reached, inclusive
// In order to ensure correctness, the timestamp of the SkipList is checked
// before and after the query.

func (sl *SkipList[K, V]) Query(ctx context.Context, start K, end K) (results []pair.Pair[K, V], err error) {
	// Check timestamp before query
	startTime := sl.timeStamp.Load()
	// Find lower bound
	_, _, succs := sl.find(start)
	// Traverse level 0 list
	curr := succs[0]
	// Regular case
	if start != end {
		for curr.key <= end && curr != sl.tail {
			select {
			case <-ctx.Done():
				{
					slog.Debug("sl: Query cancelled")
					return results, nil
				}
			default:
				{
					results = append(results, pair.Pair[K, V]{Key: curr.key, Value: curr.value})
					curr = curr.next[0].Load()
				}
			}
		}
		// Case where min, max not specified and thus all keys are queried
	} else {
		select {
		case <-ctx.Done():
			{
				slog.Debug("sl: Query cancelled")
				return results, nil
			}
		default:
			{
				for curr != sl.tail {
					results = append(results, pair.Pair[K, V]{Key: curr.key, Value: curr.value})
					curr = curr.next[0].Load()
				}
			}
		}
	}
	// Check timestamp after query
	endTime := sl.timeStamp.Load()
	if startTime != endTime {
		// Timestamp changed during query, retry
		slog.Debug("sl: Query timestamp changed during query, retrying...")
		return sl.Query(ctx, start, end)
	}
	return results, nil
}
