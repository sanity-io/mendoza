package mendoza

import (
	"testing"
)

// TestCompositeApplyErrorPropagation drives every composite applyTo whose
// first embedded sub-op can fail, so the `return err` line after the first
// failed sub-call is exercised. These composite methods normally sit at
// 66.7-57.1% because real differ-produced patches never trip the embedded
// error branch. Each block sets up a malformed patcher state, invokes the
// composite applyTo directly, and asserts ErrInvalidPatch propagates back.

func TestCompositeApplyErrorPropagation(t *testing.T) {
	// OpPushFieldCopy: OpPushField fails because the top of the input stack
	// is not a map, so getField returns ErrInvalidPatch. The error must
	// propagate out of OpPushFieldCopy.applyTo through the embedded
	// OpPushField step.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpPushFieldCopy{OpPushField: OpPushField{Index: 0}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushFieldCopy: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpPushFieldBlank: same propagation path through embedded OpPushField.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpPushFieldBlank{OpPushField: OpPushField{Index: 0}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushFieldBlank: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpPushElementCopy: embedded OpPushElement fails because input is not
	// an array.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpPushElementCopy{OpPushElement: OpPushElement{Index: 0}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushElementCopy: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpPushElementBlank: same propagation path through OpPushElement.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpPushElementBlank{OpPushElement: OpPushElement{Index: 0}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushElementBlank: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpReturnIntoObjectPop: OpReturnIntoObject reads p.outputEntry().result(),
	// pops the output stack, then expects the next outputEntry to be an
	// object. We push a string entry under a dummy entry so the pop reveals
	// a non-map and outputObject returns ErrInvalidPatch.
	{
		p := &patcher{
			outputStack: []outputEntry{
				{source: "not-an-object"}, // target after pop -> outputObject fails
				{source: "intermediate"},  // top, popped by OpReturnIntoObject
			},
		}
		op := OpReturnIntoObjectPop{OpReturnIntoObject: OpReturnIntoObject{Key: "k"}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpReturnIntoObjectPop: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpReturnIntoObjectSameKeyPop: OpReturnIntoObjectSameKey uses
	// p.inputEntry().key, pops output, then outputObject. Same setup as
	// above with a non-empty input stack so .key is reachable.
	{
		p := &patcher{
			inputStack: []inputEntry{{key: "ignored", value: "anything"}},
			outputStack: []outputEntry{
				{source: "not-an-object"},
				{source: "intermediate"},
			},
		}
		op := OpReturnIntoObjectSameKeyPop{}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpReturnIntoObjectSameKeyPop: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpReturnIntoArrayPop: OpReturnIntoArray pops then expects an array
	// underneath; we put a string there to trigger outputArray's
	// ErrInvalidPatch.
	{
		p := &patcher{
			outputStack: []outputEntry{
				{source: "not-an-array"},
				{source: "intermediate"},
			},
		}
		op := OpReturnIntoArrayPop{}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpReturnIntoArrayPop: expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpObjectCopyField: 4-step composite. First sub-step is OpPushField,
	// which fails when input is not a map -> the first `return err` is hit.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpObjectCopyField{OpPushField: OpPushField{Index: 0}}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpObjectCopyField (push): expected ErrInvalidPatch, got %v", err)
		}
	}

	// OpObjectCopyField: now trigger the *third* sub-step
	// (OpReturnIntoObjectSameKey) by giving a valid push+copy then a
	// non-map underneath the popped output. Stack shape:
	//  inputStack  = [root-map, {key:"a", value:1}] (after OpPushField)
	//  outputStack = [bad-bottom, {src:1}]          (after OpCopy)
	// OpReturnIntoObjectSameKey pops the {src:1}, then calls outputObject
	// on bad-bottom which is a string -> ErrInvalidPatch.
	{
		// We construct the state manually rather than running OpPushField
		// + OpCopy first, because that's exactly what they do.
		p := &patcher{
			inputStack: []inputEntry{
				{value: map[string]interface{}{"a": 1}},
				{key: "a", value: 1},
			},
			outputStack: []outputEntry{
				{source: "not-an-object"}, // the target object - wrong type
				{source: 1},               // pushed by an imagined OpCopy
			},
		}
		op := OpReturnIntoObjectSameKey{}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpReturnIntoObjectSameKey direct: expected ErrInvalidPatch, got %v", err)
		}
	}
}

// TestOpReturnIntoObjectErrorPath covers the bare OpReturnIntoObject.applyTo
// error branch (line 254 patcher.go), separate from the composite wrappers.
func TestOpReturnIntoObjectErrorPath(t *testing.T) {
	p := &patcher{
		outputStack: []outputEntry{
			{source: "not-an-object"},
			{source: "intermediate"},
		},
	}
	op := OpReturnIntoObject{Key: "k"}
	if err := op.applyTo(p); err != ErrInvalidPatch {
		t.Fatalf("OpReturnIntoObject: expected ErrInvalidPatch, got %v", err)
	}
}

// TestOpReturnIntoArrayErrorPath covers the bare OpReturnIntoArray.applyTo
// error branch.
func TestOpReturnIntoArrayErrorPath(t *testing.T) {
	p := &patcher{
		outputStack: []outputEntry{
			{source: "not-an-array"},
			{source: "intermediate"},
		},
	}
	op := OpReturnIntoArray{}
	if err := op.applyTo(p); err != ErrInvalidPatch {
		t.Fatalf("OpReturnIntoArray: expected ErrInvalidPatch, got %v", err)
	}
}
