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

// TestWriteSpanMatchesPatch keeps the tape encoding in writeSpan and the conversion in toOp in sync with format.go,
// for every op that can be recorded on the tape.
func TestWriteSpanMatchesPatch(t *testing.T) {
	d := differ{tape: []opRec{
		{code: codeValue, val: "value"},
		{code: codeBlank},
		{code: codePushFieldCopy, a: 1},
		{code: codePushFieldBlank, a: 2},
		{code: codePushElementCopy, a: 3},
		{code: codePushElementBlank, a: 4},
		{code: codeReturnIntoObjectPop, key: "key"},
		{code: codeReturnIntoObjectSameKeyPop},
		{code: codeReturnIntoArrayPop},
		{code: codeObjectSetFieldValue, val: 1.5, key: "set"},
		{code: codeObjectCopyField, a: 5},
		{code: codeObjectDeleteField, a: 6},
		{code: codeArrayAppendValue, val: true},
		{code: codeArrayAppendSlice, a: 7, b: 8},
		{code: codeStringAppendString, key: "str"},
		{code: codeStringAppendSlice, a: 9, b: 10},
	}}
	ops := len(d.tape)
	d.emit(opRec{code: codeInclude, a: 0, b: ops})
	all := span{ops, len(d.tape)}

	want := Patch{
		&OpValue{Value: "value"},
		&OpBlank{},
		&OpPushFieldCopy{OpPushField: OpPushField{Index: 1}},
		&OpPushFieldBlank{OpPushField: OpPushField{Index: 2}},
		&OpPushElementCopy{OpPushElement: OpPushElement{Index: 3}},
		&OpPushElementBlank{OpPushElement: OpPushElement{Index: 4}},
		&OpReturnIntoObjectPop{OpReturnIntoObject: OpReturnIntoObject{Key: "key"}},
		&OpReturnIntoObjectSameKeyPop{},
		&OpReturnIntoArrayPop{},
		&OpObjectSetFieldValue{OpValue: OpValue{Value: 1.5}, OpReturnIntoObject: OpReturnIntoObject{Key: "set"}},
		&OpObjectCopyField{OpPushField: OpPushField{Index: 5}},
		&OpObjectDeleteField{Index: 6},
		&OpArrayAppendValue{Value: true},
		&OpArrayAppendSlice{Left: 7, Right: 8},
		&OpStringAppendString{String: "str"},
		&OpStringAppendSlice{Left: 9, Right: 10},
	}
	requireSameOutput(t, "writeSpan", func(w Writer) error { return d.writeSpan(w, all) }, want)
	requireSameOutput(t, "patchFor", d.patchFor(all).WriteTo, want)
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
