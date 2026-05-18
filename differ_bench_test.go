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

// makeDistinctElement returns the i-th element of a stream of fully-distinct
// values. Used to build asymmetric benchmark inputs.
func makeDistinctElement(i int) interface{} {
	return map[string]interface{}{
		"_type": "block",
		"key":   fmt.Sprintf("k-%d", i),
		"value": fmt.Sprintf("v-%d", i),
	}
}

// BenchmarkCreateDoublePatch_RightHeavy stresses the asymmetric case where
// the target (right) slice is much larger than the source (left). Most of
// the appended elements are fresh and have no left counterpart — so
// insertAlias is rarely called, but opt #1 still allocates a `rightLen`-sized
// alias slice per candidate. This is the "array grew significantly" mutation
// shape (e.g., bulk append).
func BenchmarkCreateDoublePatch_RightHeavy(b *testing.B) {
	type sizes struct{ leftN, rightN int }
	for _, c := range []sizes{
		{100, 500},
		{100, 1000},
		{100, 5000},
	} {
		leftItems := make([]interface{}, c.leftN)
		for i := 0; i < c.leftN; i++ {
			leftItems[i] = makeDistinctElement(i)
		}
		rightItems := make([]interface{}, c.rightN)
		for i := 0; i < c.leftN; i++ {
			rightItems[i] = leftItems[i]
		}
		for i := c.leftN; i < c.rightN; i++ {
			rightItems[i] = makeDistinctElement(1_000_000 + i) // fresh, no left counterpart
		}
		left := map[string]interface{}{"items": leftItems}
		right := map[string]interface{}{"items": rightItems}

		b.Run(fmt.Sprintf("left=%d/right=%d", c.leftN, c.rightN), func(b *testing.B) {
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

// BenchmarkCreateDoublePatch_LeftHeavy is the inverse: source is much larger
// than target. Right elements all have left counterparts so insertAlias gets
// called for each, but the alias slice is only rightLen-sized so memory cost
// stays modest. This is the "array shrunk significantly" mutation shape.
func BenchmarkCreateDoublePatch_LeftHeavy(b *testing.B) {
	type sizes struct{ leftN, rightN int }
	for _, c := range []sizes{
		{500, 100},
		{1000, 100},
		{5000, 100},
	} {
		leftItems := make([]interface{}, c.leftN)
		for i := 0; i < c.leftN; i++ {
			leftItems[i] = makeDistinctElement(i)
		}
		rightItems := make([]interface{}, c.rightN)
		for i := 0; i < c.rightN; i++ {
			rightItems[i] = leftItems[i] // truncated copy of left
		}
		left := map[string]interface{}{"items": leftItems}
		right := map[string]interface{}{"items": rightItems}

		b.Run(fmt.Sprintf("left=%d/right=%d", c.leftN, c.rightN), func(b *testing.B) {
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

// BenchmarkCreateDoublePatch_ModestGrowth represents a typical real-world
// mutation: small array grows by a few elements. Both sides stay close in
// size; this is the workload mendoza should be optimal for.
func BenchmarkCreateDoublePatch_ModestGrowth(b *testing.B) {
	type sizes struct{ leftN, rightN int }
	for _, c := range []sizes{
		{100, 110},
		{500, 510},
		{1000, 1010},
	} {
		leftItems := make([]interface{}, c.leftN)
		for i := 0; i < c.leftN; i++ {
			leftItems[i] = makeDistinctElement(i)
		}
		rightItems := make([]interface{}, c.rightN)
		for i := 0; i < c.leftN; i++ {
			rightItems[i] = leftItems[i]
		}
		for i := c.leftN; i < c.rightN; i++ {
			rightItems[i] = makeDistinctElement(1_000_000 + i)
		}
		left := map[string]interface{}{"items": leftItems}
		right := map[string]interface{}{"items": rightItems}

		b.Run(fmt.Sprintf("left=%d/right=%d", c.leftN, c.rightN), func(b *testing.B) {
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
