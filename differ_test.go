package mendoza

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// recordWriter records everything written to it, for comparing serializations.
type recordWriter struct{ out []interface{} }

func (w *recordWriter) WriteUint8(v uint8) error       { w.out = append(w.out, v); return nil }
func (w *recordWriter) WriteUint(v int) error          { w.out = append(w.out, v); return nil }
func (w *recordWriter) WriteString(v string) error     { w.out = append(w.out, v); return nil }
func (w *recordWriter) WriteValue(v interface{}) error { w.out = append(w.out, v); return nil }

// sortDeletes sorts runs of OpObjectDeleteField by index. The oracle emits them in (random) map order, while the
// differ emits them in key order.
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

func formatPatch(p Patch) string {
	s := "["
	for _, op := range p {
		s += fmt.Sprintf("%T%+v ", op, op)
	}
	return s + "]"
}

func requireSamePatch(t *testing.T, what string, got, want Patch, left, right interface{}) {
	t.Helper()
	if (got == nil) != (want == nil) || !reflect.DeepEqual(sortDeletes(got), sortDeletes(want)) {
		t.Fatalf("%s: patch differs from the original differ\nleft  = %v\nright = %v\ngot   = %s\nwant  = %s",
			what, left, right, formatPatch(got), formatPatch(want))
	}
}

func requireSameOutput(t *testing.T, what string, write func(w Writer) error, want Patch) {
	t.Helper()
	var got, expected recordWriter
	if err := write(&got); err != nil {
		t.Fatal(err)
	}
	if err := want.WriteTo(&expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.out, expected.out) {
		t.Fatalf("%s: output differs from Patch.WriteTo\ngot  = %v\nwant = %v", what, got.out, expected.out)
	}
}

// checkPair checks that every way of creating a patch for left and right gives the same patches as the original
// differ. d is reused between calls to check that no state leaks from one diff to the next.
func checkPair(t *testing.T, d *Differ, left, right interface{}) {
	t.Helper()

	want, err := oracleCreatePatch(&DefaultOptions, left, right)
	if err != nil {
		t.Fatal(err)
	}
	wantForward, wantReverse, err := oracleCreateDoublePatch(&DefaultOptions, left, right)
	if err != nil {
		t.Fatal(err)
	}

	got, err := CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "CreatePatch", got, want, left, right)

	got, err = d.CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "Differ.CreatePatch", got, want, left, right)

	forward, reverse, err := CreateDoublePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "CreateDoublePatch forward", forward, wantForward, left, right)
	requireSamePatch(t, "CreateDoublePatch reverse", reverse, wantReverse, left, right)

	forward, reverse, err = d.CreateDoublePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "Differ.CreateDoublePatch forward", forward, wantForward, left, right)
	requireSamePatch(t, "Differ.CreateDoublePatch reverse", reverse, wantReverse, left, right)

	// The streaming API must write exactly what Patch.WriteTo writes for the same patch.
	if err := d.Diff(left, right); err != nil {
		t.Fatal(err)
	}
	requireSameOutput(t, "Diff+WriteTo", d.WriteTo, sortDeletes(got))

	if err := d.DiffDouble(left, right); err != nil {
		t.Fatal(err)
	}
	requireSameOutput(t, "DiffDouble+WriteTo", d.WriteTo, sortDeletes(forward))
	requireSameOutput(t, "DiffDouble+WriteReverseTo", d.WriteReverseTo, sortDeletes(reverse))

	if result := ApplyPatch(left, got); !reflect.DeepEqual(result, right) {
		t.Fatalf("patch doesn't produce right\nleft   = %v\nright  = %v\nresult = %v", left, right, result)
	}
	if result := ApplyPatch(right, reverse); !reflect.DeepEqual(result, left) {
		t.Fatalf("reverse patch doesn't produce left\nleft   = %v\nright  = %v\nresult = %v", left, right, result)
	}
}

func TestMatchesOriginalRandom(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	d := &Differ{}
	for i := 0; i < 20000; i++ {
		left := norm(randVal(r, 0))
		right := norm(mutate(r, left, 0))
		checkPair(t, d, left, right)
	}
}

func TestMatchesOriginalDocuments(t *testing.T) {
	d := &Differ{}
	for _, n := range []int{1, 2, 5, 10, 100, 1000} {
		checkPair(t, d, makeDoc(n, false), makeDoc(n, true))
		left, right := heavyPair(n)
		checkPair(t, d, left, right)
		checkPair(t, d, right, left)
		left2, right2 := keylessSpanPair(n)
		checkPair(t, d, left2, right2)
		checkPair(t, d, right2, left2)
	}
}

func TestMatchesOriginalNil(t *testing.T) {
	d := &Differ{}
	doc := map[string]interface{}{"a": "b"}
	checkPair(t, d, nil, nil)
	checkPair(t, d, nil, doc)
	checkPair(t, d, doc, nil)
	checkPair(t, d, doc, doc)
	checkPair(t, d, "a", nil)
	checkPair(t, d, nil, "a")
}

func TestDifferUnchangedSkipsIndex(t *testing.T) {
	for _, double := range []bool{false, true} {
		t.Run(fmt.Sprintf("double=%t", double), func(t *testing.T) {
			d := &Differ{}
			left, right := makeDoc(10, false), makeDoc(10, false)
			if double {
				if err := d.DiffDouble(left, right); err != nil {
					t.Fatal(err)
				}
			} else if err := d.Diff(left, right); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.hashIndex, (&Differ{}).hashIndex) {
				t.Fatal("unchanged documents should not build a hash index")
			}
			requireSameOutput(t, "unchanged forward", d.WriteTo, Patch{})
			requireSameOutput(t, "unchanged reverse", d.WriteReverseTo, Patch{})

			// A subsequent changed document must still build its index and produce the right patch.
			checkPair(t, d, left, makeDoc(10, true))
		})
	}
}

type wrapped struct{ value interface{} }

func TestConvertFunc(t *testing.T) {
	options := DefaultOptions.WithConvertFunc(func(value interface{}) interface{} {
		if w, ok := value.(wrapped); ok {
			return w.value
		}
		return value
	})
	left := map[string]interface{}{"a": wrapped{"hello"}, "b": []interface{}{wrapped{1.0}, "x"}}
	right := map[string]interface{}{"a": wrapped{"hello world"}, "b": []interface{}{"x", wrapped{2.0}}}

	want, err := oracleCreatePatch(&options, left, right)
	if err != nil {
		t.Fatal(err)
	}
	got, err := options.CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "Options.CreatePatch", got, want, left, right)

	got, err = options.NewDiffer().CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "Differ.CreatePatch", got, want, left, right)

	if _, err := CreatePatch(left, right); err == nil {
		t.Fatal("expected an error for an unsupported type without the convert function")
	}
}

func TestChildPatch(t *testing.T) {
	d := differ{reqs: []request{
		{contextIdx: 0, hasPatch: true},
		{contextIdx: 1},
		{contextIdx: 1, hasPatch: true},
		{contextIdx: 2, hasPatch: true},
	}}
	for _, test := range []struct {
		name       string
		reqs       span
		contextIdx int
		wantIdx    int
	}{
		{"first match without patch hides a later match", span{1, 4}, 1, -1},
		{"first match with patch", span{2, 4}, 1, 2},
		{"missing", span{0, 4}, 3, -1},
		{"before range", span{1, 4}, 0, -1},
		{"after range", span{0, 3}, 2, -1},
		{"empty range", span{1, 1}, 1, -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var want *request
			if test.wantIdx >= 0 {
				want = &d.reqs[test.wantIdx]
			}
			if got := d.childPatch(test.reqs, test.contextIdx); got != want {
				t.Fatalf("request = %v, want index %d", got, test.wantIdx)
			}
		})
	}
}

func TestDifferZeroValue(t *testing.T) {
	var d Differ
	requireSameOutput(t, "initial forward", d.WriteTo, Patch{})
	requireSameOutput(t, "initial reverse", d.WriteReverseTo, Patch{})
	left, right := heavyPair(10)
	checkPair(t, &d, left, right)
}

func TestDifferPatchesSurviveReuse(t *testing.T) {
	left, right := heavyPair(10)
	d := &Differ{}
	single, err := d.CreatePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	forward, reverse, err := d.CreateDoublePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}

	// Overwrite the tape, grow it, and finally clear the current results.
	if err := d.DiffDouble("before", "after"); err != nil {
		t.Fatal(err)
	}
	largeLeft, largeRight := heavyPair(100)
	if err := d.DiffDouble(largeLeft, largeRight); err != nil {
		t.Fatal(err)
	}
	if err := d.Diff(nil, nil); err != nil {
		t.Fatal(err)
	}

	wantForward, wantReverse, err := oracleCreateDoublePatch(&DefaultOptions, left, right)
	if err != nil {
		t.Fatal(err)
	}
	requireSamePatch(t, "single after reuse", single, wantForward, left, right)
	requireSamePatch(t, "forward after reuse", forward, wantForward, left, right)
	requireSamePatch(t, "reverse after reuse", reverse, wantReverse, right, left)
	if got := ApplyPatch(left, forward); !reflect.DeepEqual(got, right) {
		t.Fatal("forward patch changed after reuse")
	}
	if got := ApplyPatch(right, reverse); !reflect.DeepEqual(got, left) {
		t.Fatal("reverse patch changed after reuse")
	}
}

func TestDifferDiscardsPreviousResults(t *testing.T) {
	left, right := heavyPair(10)
	invalid := map[string]interface{}{"a": struct{}{}}
	for _, test := range []struct {
		name        string
		left, right interface{}
		double      bool
		wantError   bool
	}{
		{"forward only", left, right, false, false},
		{"equal forward", left, left, false, false},
		{"equal double", left, left, true, false},
		{"nil forward", nil, nil, false, false},
		{"nil double", nil, nil, true, false},
		{"error forward", invalid, right, false, true},
		{"error double", left, invalid, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &Differ{}
			if err := d.DiffDouble(left, right); err != nil {
				t.Fatal(err)
			}
			diff := d.Diff
			if test.double {
				diff = d.DiffDouble
			}
			if err := diff(test.left, test.right); (err != nil) != test.wantError {
				t.Fatalf("diff error = %v, want error = %t", err, test.wantError)
			}
			var forward, reverse Patch
			if !test.wantError {
				var err error
				if test.double {
					forward, reverse, err = oracleCreateDoublePatch(&DefaultOptions, test.left, test.right)
				} else {
					forward, err = oracleCreatePatch(&DefaultOptions, test.left, test.right)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				requireSameOutput(t, "forward", d.WriteTo, sortDeletes(forward))
				requireSameOutput(t, "reverse", d.WriteReverseTo, sortDeletes(reverse))
			}
		})
	}
}

func TestDifferErrors(t *testing.T) {
	d := &Differ{}
	valid := map[string]interface{}{"a": "b"}
	invalid := map[string]interface{}{"a": struct{}{}}

	if err := d.Diff(invalid, valid); err == nil {
		t.Error("expected an error for an unsupported type on the left")
	}
	if err := d.Diff(valid, invalid); err == nil {
		t.Error("expected an error for an unsupported type on the right")
	}
	if err := d.DiffDouble(valid, invalid); err == nil {
		t.Error("expected an error from DiffDouble")
	}
	if _, err := d.CreatePatch(valid, invalid); err == nil {
		t.Error("expected an error from CreatePatch")
	}
	if _, _, err := d.CreateDoublePatch(valid, invalid); err == nil {
		t.Error("expected an error from CreateDoublePatch")
	}

	// A failed diff must not leave anything behind.
	var w recordWriter
	if err := d.WriteTo(&w); err != nil || len(w.out) != 0 {
		t.Errorf("WriteTo after an error wrote %v (err %v), want nothing", w.out, err)
	}

	// And the Differ must still work.
	checkPair(t, d, valid, map[string]interface{}{"a": "c"})
}

// failingWriter fails after n writes.
type failingWriter struct{ n int }

var errWriteFailed = fmt.Errorf("write failed")

func (w *failingWriter) write() error {
	if w.n == 0 {
		return errWriteFailed
	}
	w.n--
	return nil
}

func (w *failingWriter) WriteUint8(v uint8) error       { return w.write() }
func (w *failingWriter) WriteUint(v int) error          { return w.write() }
func (w *failingWriter) WriteString(v string) error     { return w.write() }
func (w *failingWriter) WriteValue(v interface{}) error { return w.write() }

func TestWriteToReturnsWriterErrors(t *testing.T) {
	left, right := heavyPair(100)
	d := &Differ{}
	forward, reverse, err := d.CreateDoublePatch(left, right)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		write func(Writer) error
		want  Patch
	}{
		{"forward", d.WriteTo, forward},
		{"reverse", d.WriteReverseTo, reverse},
	} {
		t.Run(test.name, func(t *testing.T) {
			var all recordWriter
			if err := test.write(&all); err != nil {
				t.Fatal(err)
			}
			// Fail at every possible point, then retry on a fresh writer.
			for n := 0; n < len(all.out); n++ {
				if err := test.write(&failingWriter{n: n}); err != errWriteFailed {
					t.Fatalf("failing after %d writes: got error %v, want %v", n, err, errWriteFailed)
				}
				requireSameOutput(t, "retry", test.write, test.want)
			}
		})
	}
}
