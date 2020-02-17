package mendoza

//go-sumtype:decl Op

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

type OpReturnIntoObjectKeyless struct {
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

type OpReturnIntoObjectKeylessPop struct {
	OpReturnIntoObjectKeyless
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
	OpReturnIntoObjectKeyless
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