package skiplist

import (
	"cmp"
	"context"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Helper function to check if the skiplist maintains its sorted order
func checkSkipListOrder[K cmp.Ordered, V any](t *testing.T, sl *SkipList[K, V], expected []K) {
	ctx := context.TODO()
	result, err := sl.Query(ctx, expected[0], expected[len(expected)-1])
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result) != len(expected) {
		t.Fatalf("SkipList has incorrect length. Expected: %d, got: %d", len(expected), len(result))
	}

	for i, pair := range result {
		if pair.Key != expected[i] {
			t.Errorf("SkipList order incorrect at index %d: expected %v, got %v", i, expected[i], pair.Key)
		}
	}
}

// Helper function to check if all elements in higher levels are also present in lower levels
func checkSkipListProperty[K cmp.Ordered, V any](t *testing.T, sl *SkipList[K, V]) {
	// For each level, we will ensure that elements in level n are also in level 0 (the base level)
	for level := MAX_LEVEL - 1; level > 0; level-- {
		// Traverse the level `level` list
		currNode := sl.head.next[level].Load()
		for currNode != nil && currNode.next[level].Load() != nil {
			// Find each node at the current level in the base level (level 0)
			foundLevel, _, _ := sl.find(currNode.key)
			if foundLevel < 0 {
				t.Errorf("Node with key %v in level %d is not found in lower level 0", currNode.key, level)
			}
			currNode = currNode.next[level].Load()
		}
	}
}

// Test Sequential Operations on SkipList
func TestSkipListSequential(t *testing.T) {
	// Initialize skip list
	sl := New[int, string](0, 100)

	// Insert some elements sequentially
	keys := []int{10, 20, 30, 40, 50}
	for _, key := range keys {
		success, err := sl.Upsert(key, func(k int, currValue string, exists bool) (string, error) {
			return "Value" + strconv.Itoa(k), nil
		})
		if err != nil || !success {
			t.Fatalf("Upsert failed for key %d: %v", key, err)
		}
	}

	// Verify the correct order
	checkSkipListOrder(t, sl, keys)

	// Verify the skip list property
	checkSkipListProperty(t, sl)

	// Remove an element and check order
	removedValue, removed := sl.Remove(20)
	if !removed {
		t.Fatalf("Failed to remove key 20")
	}
	if removedValue != "Value20" {
		t.Fatalf("Incorrect value removed: expected %v, got %v", "Value20", removedValue)
	}

	// Check the skip list order after removal
	checkSkipListOrder(t, sl, []int{10, 30, 40, 50})

	// Verify the skip list property after removal
	checkSkipListProperty(t, sl)
}

// Test Concurrent Operations on SkipList
func TestSkipListConcurrent(t *testing.T) {
	for i := 0; i < 5; i++ {
		sl := New[int, string](0, 501)
		var wg sync.WaitGroup
		numGoroutines := 500

		// Perform concurrent inserts
		wg.Add(numGoroutines)
		for i := 1; i <= numGoroutines; i++ {
			go func(i int) {
				defer wg.Done()
				sl.Upsert(i, func(k int, currValue string, exists bool) (string, error) {
					return "Value" + strconv.Itoa(k), nil
				})
			}(i)
		}

		wg.Wait()

		// Perform concurrent finds
		wg.Add(numGoroutines)
		for i := 1; i <= numGoroutines; i++ {
			go func(i int) {
				defer wg.Done()
				value, found := sl.Find(i)
				if !found || value != "Value"+strconv.Itoa(i) {
					t.Errorf("Find failed for key %d, expected Value%d, got instead %s", i, i, value)
				}
			}(i)
		}

		wg.Wait()

		// Perform concurrent removals
		wg.Add(numGoroutines)
		for i := 1; i <= numGoroutines; i++ {
			go func(i int) {
				defer wg.Done()
				sl.Remove(i)
			}(i)
		}

		wg.Wait()

		// Check that the list is empty
		_, found := sl.Find(1)
		if found {
			t.Errorf("Expected list to be empty after concurrent removals")
		}

		// Verify the skip list property
		checkSkipListProperty(t, sl)
	}
}

// Test Query With End Key Beyond Inserted Range
func TestSkipListQueryBeyondRange(t *testing.T) {
	// Initialize skip list
	sl := New[int, string](0, 100)

	// Insert some elements sequentially
	keys := []int{10, 20, 30, 40, 50}
	for _, key := range keys {
		success, err := sl.Upsert(key, func(k int, currValue string, exists bool) (string, error) {
			return "Value" + strconv.Itoa(k), nil
		})
		if err != nil || !success {
			t.Fatalf("Upsert failed for key %d: %v", key, err)
		}
	}

	// Query with an end key beyond the inserted range
	ctx := context.TODO()
	startKey := 15
	endKey := 100 // Beyond the highest key (50) inserted
	result, err := sl.Query(ctx, startKey, endKey)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Expected keys in the result should be 20, 30, 40, 50 (since 15 is between 10 and 20)
	expectedKeys := []int{20, 30, 40, 50}
	if len(result) != len(expectedKeys) {
		t.Fatalf("SkipList query returned incorrect number of results. Expected: %d, got: %d", len(expectedKeys), len(result))
	}

	// Verify the keys in the result
	for i, pair := range result {
		if pair.Key != expectedKeys[i] {
			t.Errorf("SkipList query result incorrect at index %d: expected %v, got %v", i, expectedKeys[i], pair.Key)
		}
	}

	// Verify the skip list property
	checkSkipListProperty(t, sl)
}

// Test a random interleaving of operation s
func TestSkipListInterleavedOperations(t *testing.T) {
	for iteration := 0; iteration < 5; iteration++ {
		sl := New[int, string](0, 1000)
		var wg sync.WaitGroup
		numGoroutines := 200
		operations := []string{"upsert", "find", "remove", "query"}

		// Seed random number generator
		rand.Seed(time.Now().UnixNano())

		// Perform interleaved operations concurrently
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(threadID int) {
				defer wg.Done()

				// Randomly choose an operation
				op := operations[rand.Intn(len(operations))]
				key := rand.Intn(1000) // Random key for operations

				switch op {
				case "upsert":
					sl.Upsert(key, func(k int, currValue string, exists bool) (string, error) {
						return "Value" + strconv.Itoa(k), nil
					})

				case "find":
					value, found := sl.Find(key)
					if found && value != "Value"+strconv.Itoa(key) {
						t.Errorf("Find failed for key %d, expected Value%d, got %s", key, key, value)
					}

				case "remove":
					sl.Remove(key)

				case "query":
					ctx := context.TODO()
					startKey := rand.Intn(500)          // Random start key
					endKey := startKey + rand.Intn(500) // Random end key
					_, err := sl.Query(ctx, startKey, endKey)
					if err != nil {
						t.Errorf("Query failed: %v", err)
					}
				}
			}(i)
		}

		wg.Wait()

		// Final verification
		t.Logf("Finished iteration %d", iteration)

		// Ensure no deadlocks or nil pointers by performing a final check
		_, found := sl.Find(rand.Intn(1000))
		if found {
			t.Logf("Final check: skiplist contains elements")
		}

		checkSkipListProperty(t, sl)
	}
}
