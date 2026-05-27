package mendoza

import (
	"testing"

	"github.com/sanity-io/mendoza/internal/mendoza"
)

// TestMapCandidateIsMissing exercises the (currently unused, externally) helper
// methods on mapCandidate. They are part of the differ's internal interface; we
// test them directly to lock their observed behavior in.
func TestMapCandidateIsMissing(t *testing.T) {
	mc := &mapCandidate{}
	mc.init(7, 3)

	// Before any aliases are inserted, every key is missing.
	if !mc.IsMissing(mendoza.MapEntryReference(0, "foo")) {
		t.Fatalf("expected IsMissing for unaliased key to be true")
	}

	// After insertAlias, the key is no longer missing.
	mc.insertAlias(
		mendoza.MapEntryReference(0, "foo"),
		mendoza.MapEntryReference(1, "foo"),
		1,
	)
	if mc.IsMissing(mendoza.MapEntryReference(0, "foo")) {
		t.Fatalf("expected IsMissing for aliased key to be false")
	}
	// A different key remains missing.
	if !mc.IsMissing(mendoza.MapEntryReference(0, "bar")) {
		t.Fatalf("expected IsMissing for different key to remain true")
	}
}

func TestMapCandidateRegisterRequest(t *testing.T) {
	mc := &mapCandidate{}
	mc.init(0, 0)

	ref := mendoza.MapEntryReference(2, "hello")
	mc.RegisterRequest(0, ref, 0)

	if _, ok := mc.seenKeys["hello"]; !ok {
		t.Fatalf("expected RegisterRequest to record key 'hello' in seenKeys")
	}
}

// TestPatcherInputObject covers both branches of patcher.inputObject: the
// success branch where the top of the inputStack holds a map, and the error
// branch where it holds a non-map value.
func TestPatcherInputObject(t *testing.T) {
	// Success case.
	p := &patcher{
		inputStack: []inputEntry{
			{value: map[string]interface{}{"a": 1}},
		},
	}
	obj, err := p.inputObject()
	if err != nil {
		t.Fatalf("unexpected error from inputObject: %v", err)
	}
	if obj["a"] != 1 {
		t.Fatalf("expected obj[a]=1, got %v", obj["a"])
	}

	// Error case: top of stack is not a map.
	p2 := &patcher{
		inputStack: []inputEntry{
			{value: "not-a-map"},
		},
	}
	obj2, err := p2.inputObject()
	if err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch, got %v", err)
	}
	if obj2 != nil {
		t.Fatalf("expected nil obj, got %v", obj2)
	}
}

// TestPatcherInputArrayString covers the error branches of inputArray and
// inputString when the top of the inputStack does not hold the expected type.
func TestPatcherInputArrayString(t *testing.T) {
	p := &patcher{
		inputStack: []inputEntry{
			{value: 42},
		},
	}
	if _, err := p.inputArray(); err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch from inputArray, got %v", err)
	}
	if _, err := p.inputString(); err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch from inputString, got %v", err)
	}
}

// TestOutputObjectArrayStringErrors exercises the error branches of
// outputObject, outputArray and outputString when the output entry's source is
// of the wrong type.
func TestOutputObjectArrayStringErrors(t *testing.T) {
	p := &patcher{
		outputStack: []outputEntry{
			{source: 42}, // not a map, array or string
		},
	}
	if _, err := p.outputObject(); err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch from outputObject, got %v", err)
	}

	// outputArray and outputString each get a fresh stack since the prior call
	// to outputObject may have mutated the writable fields.
	p2 := &patcher{
		outputStack: []outputEntry{
			{source: 42},
		},
	}
	if _, err := p2.outputArray(); err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch from outputArray, got %v", err)
	}

	p3 := &patcher{
		outputStack: []outputEntry{
			{source: 42},
		},
	}
	if _, err := p3.outputString(); err != ErrInvalidPatch {
		t.Fatalf("expected ErrInvalidPatch from outputString, got %v", err)
	}
}

// TestApplyToErrorPaths exercises the error returns inside the various applyTo
// methods that propagate ErrInvalidPatch when their input/output expectations
// aren't met. These short circuits are otherwise hit only via malformed
// synthetic patches.
func TestApplyToErrorPaths(t *testing.T) {
	// OpArrayAppendValue: outputArray fails because source is not array.
	{
		p := &patcher{
			outputStack: []outputEntry{{source: 42}},
		}
		op := OpArrayAppendValue{Value: "x"}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpArrayAppendValue: expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpArrayAppendSlice: inputArray fails because input is not array.
	{
		p := &patcher{
			inputStack:  []inputEntry{{value: 42}},
			outputStack: []outputEntry{{source: []interface{}{}}},
		}
		op := OpArrayAppendSlice{Left: 0, Right: 1}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpArrayAppendSlice (input): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpArrayAppendSlice: bounds check failure.
	{
		p := &patcher{
			inputStack:  []inputEntry{{value: []interface{}{1, 2}}},
			outputStack: []outputEntry{{source: []interface{}{}}},
		}
		op := OpArrayAppendSlice{Left: 0, Right: 99}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpArrayAppendSlice (bounds): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpStringAppendString: outputString fails because source is not a string.
	{
		p := &patcher{
			outputStack: []outputEntry{{source: 42}},
		}
		op := OpStringAppendString{String: "x"}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpStringAppendString: expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpStringAppendSlice: inputString fails because input is not a string.
	{
		p := &patcher{
			inputStack:  []inputEntry{{value: 42}},
			outputStack: []outputEntry{{source: ""}},
		}
		op := OpStringAppendSlice{Left: 0, Right: 1}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpStringAppendSlice (input): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpStringAppendSlice: bounds check failure.
	{
		p := &patcher{
			inputStack:  []inputEntry{{value: "hi"}},
			outputStack: []outputEntry{{source: ""}},
		}
		op := OpStringAppendSlice{Left: 0, Right: 99}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpStringAppendSlice (bounds): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpPushElement: out-of-bounds index.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: []interface{}{1}}},
		}
		op := OpPushElement{Index: 99}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushElement (bounds): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpPushElement: input is not an array.
	{
		p := &patcher{
			inputStack: []inputEntry{{value: 42}},
		}
		op := OpPushElement{Index: 0}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushElement (not array): expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpPushParent: invalid N (negative resulting index).
	{
		p := &patcher{
			inputStack: []inputEntry{{value: "x"}},
		}
		op := OpPushParent{N: 99}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpPushParent: expected ErrInvalidPatch, got %v", err)
		}
	}
	// OpObjectDeleteField: bad field index.
	{
		p := &patcher{
			inputStack:  []inputEntry{{value: map[string]interface{}{"a": 1}}},
			outputStack: []outputEntry{{source: map[string]interface{}{"a": 1}}},
		}
		op := OpObjectDeleteField{Index: 99}
		if err := op.applyTo(p); err != ErrInvalidPatch {
			t.Fatalf("OpObjectDeleteField: expected ErrInvalidPatch, got %v", err)
		}
	}
}
