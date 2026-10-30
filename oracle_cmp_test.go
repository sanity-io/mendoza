package mendoza

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// recWriter records everything written, for comparing serializations.
type recWriter struct{ out []interface{} }

func (w *recWriter) WriteUint8(v uint8) error       { w.out = append(w.out, v); return nil }
func (w *recWriter) WriteUint(v int) error          { w.out = append(w.out, v); return nil }
func (w *recWriter) WriteString(v string) error     { w.out = append(w.out, v); return nil }
func (w *recWriter) WriteValue(v interface{}) error { w.out = append(w.out, v); return nil }

// nopWriter discards everything without allocating.
type nopWriter struct{ n int }

func (w *nopWriter) WriteUint8(v uint8) error       { w.n++; return nil }
func (w *nopWriter) WriteUint(v int) error          { w.n++; return nil }
func (w *nopWriter) WriteString(v string) error     { w.n++; return nil }
func (w *nopWriter) WriteValue(v interface{}) error { w.n++; return nil }

// sortDeletes normalizes runs of OpObjectDeleteField: the original emitted
// them in random map order, the new differ emits them in key order.
func sortDeletes(p Patch) Patch {
	out := append(Patch{}, p...)
	for i := 0; i < len(out); {
		j := i
		for j < len(out) {
			if _, ok := out[j].(*OpObjectDeleteField); !ok {
				break
			}
			j++
		}
		if j > i {
			run := out[i:j]
			sort.Slice(run, func(a, b int) bool {
				return run[a].(*OpObjectDeleteField).Index < run[b].(*OpObjectDeleteField).Index
			})
			i = j
		} else {
			i++
		}
	}
	return out
}

func samePatch(t *testing.T, what string, got, want Patch, l, r interface{}) {
	t.Helper()
	got, want = sortDeletes(got), sortDeletes(want)
	if (got == nil) != (want == nil) || !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: patch differs from oracle\nL=%v\nR=%v\ngot =%s\nwant=%s", what, l, r, fmtPatch(got), fmtPatch(want))
	}
}

func fmtPatch(p Patch) string {
	s := "["
	for _, op := range p {
		s += fmt.Sprintf("%T%+v ", op, op)
	}
	return s + "]"
}

func checkPair(t *testing.T, d *Differ, l, r interface{}) {
	t.Helper()
	want, err := oCreatePatchDefault(l, r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CreatePatch(l, r)
	if err != nil {
		t.Fatal(err)
	}
	samePatch(t, "CreatePatch", got, want, l, r)

	got2, err := d.CreatePatch(l, r) // reused Differ
	if err != nil {
		t.Fatal(err)
	}
	samePatch(t, "Differ.CreatePatch", got2, want, l, r)

	wf, wb, _ := oCreateDoublePatchDefault(l, r)
	gf, gb, _ := d.CreateDoublePatch(l, r)
	samePatch(t, "double fwd", gf, wf, l, r)
	samePatch(t, "double back", gb, wb, l, r)

	// Streaming output must serialize identically to Patch.WriteTo.
	if l != nil {
		if err := d.Diff(l, r); err != nil {
			t.Fatal(err)
		}
		var a, b recWriter
		d.WriteTo(&a)
		got.WriteTo(&b)
		if !reflect.DeepEqual(a.out, b.out) {
			t.Fatalf("WriteTo differs\n%v\n%v", a.out, b.out)
		}
	}
}

func TestOracleRandom(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	d := NewDiffer()
	for i := 0; i < 100000; i++ {
		l := norm(randVal(r, 0))
		rt := norm(mutate(r, l, 0))
		checkPair(t, d, l, rt)
	}
}

func TestOracleDocs(t *testing.T) {
	d := NewDiffer()
	for _, n := range []int{1, 2, 5, 10, 100, 1000} {
		checkPair(t, d, makeDoc(n, false), makeDoc(n, true))
		l, r := heavyPair(n)
		checkPair(t, d, l, r)
		checkPair(t, d, r, l)
	}
}

func TestZeroAllocDiff(t *testing.T) {
	for _, n := range []int{10, 100, 1000} {
		for name, pair := range map[string][2]interface{}{
			"single": {makeDoc(n, false), makeDoc(n, true)},
			"heavy":  func() [2]interface{} { l, r := heavyPair(n); return [2]interface{}{l, r} }(),
		} {
			d := NewDiffer()
			w := &nopWriter{}
			run := func() {
				if err := d.DiffDouble(pair[0], pair[1]); err != nil {
					t.Fatal(err)
				}
				d.WriteTo(w)
				d.WriteReverseTo(w)
			}
			run() // warm up
			if a := testing.AllocsPerRun(50, run); a != 0 {
				t.Errorf("%s/%d: %v allocs per DiffDouble+WriteTo, want 0", name, n, a)
			}
		}
	}
}
