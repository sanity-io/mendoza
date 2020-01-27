package mendoza

type EnterType uint8

const (
	EnterNop EnterType = iota
	EnterCopy
	EnterBlank
)

//go-sumtype:decl Op

type Op interface {
	isOp()
}

type Patch []Op

// Stack operators

type OpEnterField struct {
	Enter EnterType
	Index int
}

type OpEnterElement struct {
	Enter EnterType
	Index int
}

type OpEnterRoot struct {
	Enter EnterType
}

type OpEnterValue struct {
	Value interface{}
}

type OpReturnIntoObject struct {
	Key string
}

type OpReturnIntoArray struct {
}

// Object helpers

type OpObjectSetFieldValue struct {
	Key   string
	Value interface{}
}

type OpObjectCopyField struct {
	Index int
}

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

// isOp() implementations:
func (OpEnterRoot) isOp()           {}
func (OpEnterValue) isOp()          {}
func (OpEnterField) isOp()          {}
func (OpEnterElement) isOp()        {}
func (OpReturnIntoArray) isOp()     {}
func (OpReturnIntoObject) isOp()    {}
func (OpObjectSetFieldValue) isOp() {}
func (OpObjectCopyField) isOp()     {}
func (OpObjectDeleteField) isOp()   {}
func (OpArrayAppendValue) isOp()    {}
func (OpArrayAppendSlice) isOp()    {}
