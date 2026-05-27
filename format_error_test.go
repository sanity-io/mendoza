package mendoza

import (
	"errors"
	"testing"
)

// These tests exercise the error-propagation branches between the sub-calls
// of the multi-step readParams/writeParams in format.go and the top-level
// WriteTo error paths. The existing roundtrip suite exercises only the
// happy-path side of each `if err != nil { return }`, leaving the
// composite codec functions stuck at 75-80%.
//
// We use a synthetic Reader/Writer that returns success for the first
// `failAt` calls and then injects an error, so each codec method can be
// driven directly without constructing real wire bytes.

var errCodecInject = errors.New("mendoza codec inject")

type fakeFailingReader struct {
	calls  int
	failAt int // first call number that returns errCodecInject (1-based)
}

func (r *fakeFailingReader) tick() error {
	r.calls++
	if r.calls > r.failAt {
		return errCodecInject
	}
	return nil
}

func (r *fakeFailingReader) ReadUint8() (uint8, error) {
	if err := r.tick(); err != nil {
		return 0, err
	}
	return 0, nil
}

func (r *fakeFailingReader) ReadUint() (int, error) {
	if err := r.tick(); err != nil {
		return 0, err
	}
	return 0, nil
}

func (r *fakeFailingReader) ReadString() (string, error) {
	if err := r.tick(); err != nil {
		return "", err
	}
	return "", nil
}

func (r *fakeFailingReader) ReadValue() (interface{}, error) {
	if err := r.tick(); err != nil {
		return nil, err
	}
	return nil, nil
}

type fakeFailingWriter struct {
	calls  int
	failAt int
}

func (w *fakeFailingWriter) tick() error {
	w.calls++
	if w.calls > w.failAt {
		return errCodecInject
	}
	return nil
}

func (w *fakeFailingWriter) WriteUint8(uint8) error      { return w.tick() }
func (w *fakeFailingWriter) WriteUint(int) error         { return w.tick() }
func (w *fakeFailingWriter) WriteString(string) error    { return w.tick() }
func (w *fakeFailingWriter) WriteValue(interface{}) error { return w.tick() }

// TestReadParamsCompositeErrorBranch hits the `if err != nil { return }`
// between sub-reads inside the composite readParams implementations.
func TestReadParamsCompositeErrorBranch(t *testing.T) {
	cases := []struct {
		name string
		run  func(r Reader) error
	}{
		{"OpObjectSetFieldValue", func(r Reader) error { return (&OpObjectSetFieldValue{}).readParams(r) }},
		{"OpObjectCopyField", func(r Reader) error { return (&OpObjectCopyField{}).readParams(r) }},
		{"OpArrayAppendSlice", func(r Reader) error { return (&OpArrayAppendSlice{}).readParams(r) }},
		{"OpStringAppendSlice", func(r Reader) error { return (&OpStringAppendSlice{}).readParams(r) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeFailingReader{failAt: 0} // fail on first sub-read
			err := tc.run(r)
			if !errors.Is(err, errCodecInject) {
				t.Fatalf("%s readParams: expected injected error, got %v", tc.name, err)
			}
		})
	}
}

// TestWriteParamsCompositeErrorBranch is the symmetric writer-side test.
func TestWriteParamsCompositeErrorBranch(t *testing.T) {
	cases := []struct {
		name string
		run  func(w Writer) error
	}{
		{"OpObjectSetFieldValue", func(w Writer) error { return (&OpObjectSetFieldValue{}).writeParams(w) }},
		{"OpObjectCopyField", func(w Writer) error { return (&OpObjectCopyField{}).writeParams(w) }},
		{"OpArrayAppendSlice", func(w Writer) error { return (&OpArrayAppendSlice{}).writeParams(w) }},
		{"OpStringAppendSlice", func(w Writer) error { return (&OpStringAppendSlice{}).writeParams(w) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &fakeFailingWriter{failAt: 0} // fail on first sub-write
			err := tc.run(w)
			if !errors.Is(err, errCodecInject) {
				t.Fatalf("%s writeParams: expected injected error, got %v", tc.name, err)
			}
		})
	}
}

// TestWriteToOpcodeWriteError covers WriteTo's `if err != nil { return err }`
// branch immediately after w.WriteUint8(code).
func TestWriteToOpcodeWriteError(t *testing.T) {
	w := &fakeFailingWriter{failAt: 0}
	err := WriteTo(w, &OpCopy{})
	if !errors.Is(err, errCodecInject) {
		t.Fatalf("WriteTo opcode error: expected injected error, got %v", err)
	}
}

// TestWriteToParamsWriteError covers WriteTo's `if err != nil { return err }`
// branch after op.writeParams(w). OpValue.writeParams calls w.WriteValue,
// so failAt=1 lets the opcode write succeed and then fails the params write.
func TestWriteToParamsWriteError(t *testing.T) {
	w := &fakeFailingWriter{failAt: 1}
	err := WriteTo(w, &OpValue{Value: "anything"})
	if !errors.Is(err, errCodecInject) {
		t.Fatalf("WriteTo params error: expected injected error, got %v", err)
	}
}

// TestPatchWriteToReturnsError covers Patch.WriteTo's loop-internal error
// return: with a multi-op patch and a writer that fails on the first call,
// the first WriteTo invocation errors and the outer loop must propagate it.
func TestPatchWriteToReturnsError(t *testing.T) {
	patch := Patch{&OpCopy{}, &OpPop{}}
	w := &fakeFailingWriter{failAt: 0}
	err := patch.WriteTo(w)
	if !errors.Is(err, errCodecInject) {
		t.Fatalf("Patch.WriteTo: expected injected error, got %v", err)
	}
}

// TestOpObjectCopyFieldApplyReturnError covers the third `return err` branch
// of OpObjectCopyField.applyTo (patcher.go:400) where OpPushField+OpCopy
// succeed but OpReturnIntoObjectSameKey fails because the bottom of the
// output stack isn't an object.
func TestOpObjectCopyFieldApplyReturnError(t *testing.T) {
	p := &patcher{
		options:    &DefaultOptions,
		inputStack: []inputEntry{{value: map[string]interface{}{"a": 1}}},
		outputStack: []outputEntry{
			{source: "not-an-object"}, // target object — wrong type
		},
	}
	op := OpObjectCopyField{OpPushField: OpPushField{Index: 0}}
	if err := op.applyTo(p); err != ErrInvalidPatch {
		t.Fatalf("OpObjectCopyField third-step: expected ErrInvalidPatch, got %v", err)
	}
}
