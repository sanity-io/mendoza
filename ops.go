// This package implements a Mendoza differ and patcher.
//
// You use CreatePatch (or CreateDoublePatch) to create patches,
// which can then be applied with ApplyPatch.
//
// The Patch type is already JSON serializable, but you can implement Reader/Writer
// (and use WriteTo/ReadFrom) if you need a custom serialization.
//
// Supported types
//
// The differ/patcher is only implemented to work on the following types:
//  bool
//  float64
//  string
//  map[string]interface{}
//  []interface{}
//  nil
//
// If you need to support additional types you can use the option WithConvertFunc which
// defines a function that is applied to every value.
package mendoza

//go-sumtype:decl Op

// Op is the interface for an operation.
type Op interface {
	applyTo(p *patcher)
	readParams(r Reader) error
	writeParams(w Writer) error
}

// A patch is a list of operations.
type Patch []Op


// Output stack operators

type OpValue struct {
	Value interface{}
}

type OpCopy struct {
}

type OpBlank struct {
}

type OpReturnIntoObject struct {
	Key string
}

type OpReturnIntoObjectSameKey struct {
}

type OpReturnIntoArray struct {
}


// Input stack operators

type OpPushField struct {
	Index int
}

type OpPushElement struct {
	Index int
}

type OpPushParent struct {
	N int
}

type OpPop struct {
}

// Combined input and output

type OpPushFieldCopy struct {
	OpPushField
	OpCopy
}

type OpPushFieldBlank struct {
	OpPushField
	OpBlank
}

type OpPushElementCopy struct {
	OpPushElement
	OpCopy
}

type OpPushElementBlank struct {
	OpPushElement
	OpBlank
}

type OpReturnIntoObjectPop struct {
	OpReturnIntoObject
	OpPop
}

type OpReturnIntoObjectSameKeyPop struct {
	OpReturnIntoObjectSameKey
	OpPop
}

type OpReturnIntoArrayPop struct {
	OpReturnIntoArray
	OpPop
}

// Object helpers

type OpObjectSetFieldValue struct {
	OpValue
	OpReturnIntoObject
}

type OpObjectCopyField struct {
	OpPushField
	OpCopy
	OpReturnIntoObjectSameKey
	OpPop
}

//
type OpObjectDeleteField struct {
	Index int
}

// Array helpers

type OpArrayAppendValue struct {
	Value interface{}
}

type OpArrayAppendSlice struct {
	Left  int
	Right int
}

// String helpers

type OpStringAppendString struct {
	String string
}

type OpStringAppendSlice struct {
	Left  int
	Right int
}
