package pair

import "cmp"

type Pair[K cmp.Ordered, V any] struct {
	Key   K
	Value V
}
