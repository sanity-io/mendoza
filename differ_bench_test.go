package mendoza_test

import (
	"fmt"
	"testing"

	"github.com/sanity-io/mendoza"
)

// makeDuplicateHeavySlice returns a slice of n small map elements drawn
// from a pool of `distinct` distinct values. A small distinct count
// produces large hashIndex buckets in mendoza, which is the regime that
// dominates CPU profiles taken from real callers (reconstructSlice +
// sliceCandidate.insertAlias on a `map[int]sliceAlias`).
func makeDuplicateHeavySlice(n, distinct int) []interface{} {
	out := make([]interface{}, n)
	for i := 0; i < n; i++ {
		out[i] = map[string]interface{}{
			"_type": "block",
			"key":   fmt.Sprintf("k%d", i%distinct),
			"value": fmt.Sprintf("payload-%d", i%distinct),
		}
	}
	return out
}

// mutateSlice replaces every `stride`-th element with a fresh value so the
// diff is non-trivial while still leaving most elements as exact matches.
func mutateSlice(in []interface{}, stride int) []interface{} {
	out := make([]interface{}, len(in))
	copy(out, in)
	for i := 0; i < len(out); i += stride {
		out[i] = map[string]interface{}{
			"_type": "block",
			"key":   fmt.Sprintf("changed-%d", i),
			"value": fmt.Sprintf("changed-payload-%d", i),
		}
	}
	return out
}

func mutationStride(n int) int {
	if s := n / 10; s > 0 {
		return s
	}
	return 1
}

// BenchmarkCreateDoublePatch_DuplicateHeavySlice exercises the hot path
// observed in production CPU profiles: a large array of mostly-duplicate
// elements where reconstructSlice's inner triple loop dominates.
func BenchmarkCreateDoublePatch_DuplicateHeavySlice(b *testing.B) {
	for _, n := range []int{100, 500, 1000, 2000} {
		left := map[string]interface{}{"items": makeDuplicateHeavySlice(n, 8)}
		rightItems := mutateSlice(makeDuplicateHeavySlice(n, 8), mutationStride(n))
		right := map[string]interface{}{"items": rightItems}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _, err := mendoza.CreateDoublePatch(left, right)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// makePoolElement returns the i-th element of a fixed pool. Used by the
// scattered benchmark so the same value appears in multiple arrays at the
// same indices, blowing up hashIndex bucket size across the whole document.
func makePoolElement(i int) interface{} {
	return map[string]interface{}{
		"_type": "reference",
		"_ref":  fmt.Sprintf("doc-%d", i),
	}
}

// makeArrayFromPool returns an n-element slice where each slot draws from a
// `distinct`-sized pool. Combined with sibling arrays drawing from the same
// pool, this produces large hashIndex buckets whose entries are spread
// across many different parents.
func makeArrayFromPool(n, distinct int) []interface{} {
	out := make([]interface{}, n)
	for i := 0; i < n; i++ {
		out[i] = makePoolElement(i % distinct)
	}
	return out
}

// BenchmarkCreateDoublePatch_ScatteredDuplicates mirrors production CPU
// profiles where the same subtree (e.g. a reference object) appears in many
// sibling arrays. When reconstructSlice processes any one of those arrays,
// hashIndex buckets contain entries from ALL sibling arrays, but only a
// fraction match the active candidate's parent — exercising the
// `d.left.Entries[otherIdx]` cache-cold load at differ.go:621 in addition
// to insertAlias's map ops.
func BenchmarkCreateDoublePatch_ScatteredDuplicates(b *testing.B) {
	const arrays = 10
	const distinct = 4
	for _, n := range []int{100, 500, 1000} {
		doc := func() map[string]interface{} {
			d := make(map[string]interface{}, arrays)
			for a := 0; a < arrays; a++ {
				d[fmt.Sprintf("arr%d", a)] = makeArrayFromPool(n, distinct)
			}
			return d
		}
		left := doc()
		right := doc()
		// Mutate one element in each array on the right side so the diff
		// has to do real work rather than detecting equality.
		for a := 0; a < arrays; a++ {
			arr := right[fmt.Sprintf("arr%d", a)].([]interface{})
			arr[0] = map[string]interface{}{
				"_type": "reference",
				"_ref":  fmt.Sprintf("changed-arr%d", a),
			}
		}

		b.Run(fmt.Sprintf("arrays=%d/n=%d", arrays, n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _, err := mendoza.CreateDoublePatch(left, right)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCreateDoublePatch_DistinctSlice is a control: same shape, same
// size, but every element is unique so hashIndex buckets are size 1 and the
// inner loops collapse. Comparing against the duplicate-heavy benchmark
// isolates the cost paid for hash collisions.
func BenchmarkCreateDoublePatch_DistinctSlice(b *testing.B) {
	for _, n := range []int{100, 500, 1000, 2000} {
		left := map[string]interface{}{"items": makeDuplicateHeavySlice(n, n)}
		rightItems := mutateSlice(makeDuplicateHeavySlice(n, n), mutationStride(n))
		right := map[string]interface{}{"items": rightItems}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _, err := mendoza.CreateDoublePatch(left, right)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
