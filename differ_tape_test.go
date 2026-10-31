package mendoza

import (
	"reflect"
	"testing"
)

func TestUpdateRequestDiscardsRejectedPatch(t *testing.T) {
	for _, size := range []int{5, 6} {
		d := differ{reqs: []request{{size: 10}}}
		chosen := d.valueSpan("chosen")
		d.updateRequest(0, chosen, 5, 3)
		wantReq := d.reqs[0]

		// An equal or higher cost must keep the first choice and release all
		// references in the newly appended suffix, including keys and literals.
		rejected := d.valueSpan(map[string]interface{}{"value": "rejected"})
		d.emit(opRec{code: codeStringAppendString, key: "rejected"})
		rejected.end = len(d.tape)
		d.updateRequest(0, rejected, size, 4)

		if d.reqs[0] != wantReq {
			t.Fatalf("cost %d replaced the chosen request: %+v", size, d.reqs[0])
		}
		if len(d.tape) != chosen.end {
			t.Fatalf("cost %d left rejected records on the tape", size)
		}
		for _, op := range d.tape[rejected.start:rejected.end] {
			if !reflect.DeepEqual(op, opRec{}) {
				t.Fatalf("discarded record still contains data: %+v", op)
			}
		}
		requireSameOutput(t, "chosen patch", func(w Writer) error {
			return d.writeSpan(w, chosen)
		}, Patch{&OpValue{Value: "chosen"}})

		// The next accepted patch can reuse that suffix without changing earlier spans.
		next := d.valueSpan("better")
		d.updateRequest(0, next, 4, 5)
		if d.reqs[0].patch != next || d.reqs[0].size != 4 || d.reqs[0].patchSourceIdx != 5 {
			t.Fatalf("cheaper patch was not selected: %+v", d.reqs[0])
		}
	}
}

func TestRejectedReconstructionReleasesTape(t *testing.T) {
	left, right := replacementPair(1000)
	for _, test := range []struct {
		name        string
		left, right interface{}
	}{
		{"strings", left, right},
		{"map", map[string]interface{}{"a": 1.0}, map[string]interface{}{"b": 2.0}},
		// The child string patch is cheaper than its literal, but the enclosing
		// array patch is not. Its now-unreachable child records must be discarded too.
		{"accepted child", []interface{}{"abcdeA"}, []interface{}{"abcdeB"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := NewDiffer()
			for i := 0; i < 2; i++ {
				if err := d.DiffDouble(test.left, test.right); err != nil {
					t.Fatal(err)
				}
				if len(d.differ.tape) != 2 {
					t.Fatalf("retained %d records for two literal patches", len(d.differ.tape))
				}
				requireSameOutput(t, "forward", d.WriteTo, Patch{&OpValue{Value: test.right}})
				requireSameOutput(t, "reverse", d.WriteReverseTo, Patch{&OpValue{Value: test.left}})
				for _, op := range d.differ.tape[len(d.differ.tape):cap(d.differ.tape)] {
					if !reflect.DeepEqual(op, opRec{}) {
						t.Fatalf("discarded record still contains data: %+v", op)
					}
				}
			}
		})
	}
}

func TestRejectedStringDoesNotGrowTape(t *testing.T) {
	d := NewDiffer()
	if err := d.fill("before", "after"); err != nil {
		t.Fatal(err)
	}
	d.differ.left = &d.left
	d.differ.reqs = []request{{primaryIdx: 0, size: d.right.Entries[0].Size + 1}}
	d.differ.reconstructString("after", span{0, 1})
	if cap(d.differ.tape) != 0 || d.differ.reqs[0].hasPatch {
		t.Fatal("rejected string patch should not allocate or select any tape records")
	}
}

func TestWriteSpanOperandlessOps(t *testing.T) {
	d := differ{tape: []opRec{
		{code: codeBlank},
		{code: codeReturnIntoObjectSameKeyPop},
		{code: codeReturnIntoArrayPop},
	}}
	requireSameOutput(t, "operandless operations", func(w Writer) error {
		return d.writeSpan(w, span{0, len(d.tape)})
	}, Patch{&OpBlank{}, &OpReturnIntoObjectSameKeyPop{}, &OpReturnIntoArrayPop{}})
}

func TestWriteSpanRejectsUnknownOpcode(t *testing.T) {
	defer func() {
		if got := recover(); got != "mendoza: unexpected op on tape" {
			t.Fatalf("panic = %v, want unexpected-op panic", got)
		}
	}()
	d := differ{tape: []opRec{{code: 254}}}
	d.writeSpan(&recordWriter{}, span{0, 1})
}
