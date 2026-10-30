package mendoza

import (
	"fmt"
	"testing"
)

func benchPairs(b *testing.B, f func(b *testing.B, l, r interface{})) {
	for _, n := range sizes {
		l, r := makeDoc(n, false), makeDoc(n, true)
		b.Run(fmt.Sprintf("single/%d", n), func(b *testing.B) { b.ReportAllocs(); f(b, l, r) })
	}
	for _, n := range sizes {
		l, r := heavyPair(n)
		b.Run(fmt.Sprintf("heavy/%d", n), func(b *testing.B) { b.ReportAllocs(); f(b, l, r) })
	}
}

func BenchmarkV1Phase1(b *testing.B) {
	benchPairs(b, func(b *testing.B, l, r interface{}) {
		for i := 0; i < b.N; i++ {
			oCreatePatchDefault(l, r)
		}
	})
}
func BenchmarkV2CreatePatch(b *testing.B) {
	benchPairs(b, func(b *testing.B, l, r interface{}) {
		for i := 0; i < b.N; i++ {
			CreatePatch(l, r)
		}
	})
}
func BenchmarkV2DifferWriteTo(b *testing.B) {
	benchPairs(b, func(b *testing.B, l, r interface{}) {
		d := NewDiffer()
		w := &nopWriter{}
		for i := 0; i < b.N; i++ {
			d.Diff(l, r)
			d.WriteTo(w)
		}
	})
}
