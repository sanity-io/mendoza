//go:build go1.24 && !race
// +build go1.24,!race

package mendoza

import "testing"

// These assertions cover warmed-up fixtures on Go 1.24+ with the default map implementation.
// Older Go map implementations and the race detector can allocate when rebuilding the hash index.
// Correctness tests and allocation benchmarks still run on older Go versions.
func TestDiffDoesNotAllocate(t *testing.T) {
	for _, n := range []int{10, 100, 1000} {
		single := [2]interface{}{makeDoc(n, false), makeDoc(n, true)}
		left, right := heavyPair(n)
		heavy := [2]interface{}{left, right}

		for name, pair := range map[string][2]interface{}{"single": single, "heavy": heavy} {
			d := &Differ{}
			w := &discardWriter{}
			run := func() {
				if err := d.DiffDouble(pair[0], pair[1]); err != nil {
					t.Fatal(err)
				}
				if err := d.WriteTo(w); err != nil {
					t.Fatal(err)
				}
				if err := d.WriteReverseTo(w); err != nil {
					t.Fatal(err)
				}
				if err := d.Diff(pair[0], pair[1]); err != nil {
					t.Fatal(err)
				}
				if err := d.WriteTo(w); err != nil {
					t.Fatal(err)
				}
			}
			run() // grow the buffers
			if allocs := testing.AllocsPerRun(20, run); allocs != 0 {
				t.Errorf("%s/%d: %v allocations, want 0", name, n, allocs)
			}
		}
	}
}

func TestNilPatchAllocations(t *testing.T) {
	for _, test := range []struct {
		name      string
		run       func()
		maxAllocs float64
	}{
		{"empty", func() { CreatePatch(nil, nil) }, 0},
		{"insert", func() { CreatePatch(nil, "insert") }, 2},
		{"double empty", func() { CreateDoublePatch(nil, nil) }, 0},
		{"double insert", func() { CreateDoublePatch(nil, "insert") }, 4},
		{"double delete", func() { CreateDoublePatch("delete", nil) }, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Only the returned slices and OpValues may allocate; no Differ or tape.
			if allocs := testing.AllocsPerRun(100, test.run); allocs > test.maxAllocs {
				t.Errorf("%v allocations, want at most %v", allocs, test.maxAllocs)
			}
		})
	}
}

// TestCreatePatchAllocations checks that a reused Differ only allocates the returned Patch: the slice and (at most)
// one allocation per op.
func TestCreatePatchAllocations(t *testing.T) {
	left, right := heavyPair(1000)
	d := &Differ{}
	patch, err := d.CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := d.CreatePatch(left, right); err != nil {
			t.Fatal(err)
		}
	})
	if max := float64(len(patch) + 1); allocs > max {
		t.Errorf("%v allocations, want at most %v", allocs, max)
	}
}
