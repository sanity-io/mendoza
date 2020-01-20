package mendoza

type EnterType uint8

const (
	EnterNop EnterType = iota
	EnterCopy
	EnterArray
	EnterObject
	EnterFinal
)

//go-sumtype:decl Op

type Op interface {
	isOp()
}

type Patch []Op

// Stack operators

type OpEnterField struct {
	Enter EnterType
	Key   string
}

type OpEnterElement struct {
	Enter EnterType
	Index int
}

type OpEnterRoot struct {
	Enter EnterType
}

type OpReturn struct {
	Key string
}

// Object helpers

type OpSetFieldValue struct {
	Key string
	Value interface{}
}

type OpOutputValue struct {
	Value interface{}
}

type OpCopyField struct {
	Key string
}

type OpDeleteField struct {
	Key string
}

// Array helpers

type OpAppendValue struct {
	Value interface{}
}

type OpAppendSlice struct {
	Left  int
	Right int
}

// isOp() implementations:
func (OpEnterRoot) isOp()   {}
func (OpOutputValue) isOp() {}

func (OpEnterField) isOp() {}
func (OpEnterElement) isOp()  {}
func (OpReturn) isOp()        {}
func (OpSetFieldValue) isOp() {}
func (OpCopyField) isOp()     {}
func (OpDeleteField) isOp() {}
func (OpAppendValue) isOp() {}
func (OpAppendSlice) isOp() {}
