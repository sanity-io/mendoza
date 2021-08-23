package mendoza

import (
	"fmt"
	"io"
	"math"
)

// Writer is an interface for writing values. This can be used for supporting a custom serialization format.
type Writer interface {
	WriteUint8(v uint8) error
	WriteUint(v int) error
	WriteString(v string) error
	WriteValue(v interface{}) error
}

// Reader is an interface for reading values. This can be used for supporting a custom serialization format.
type Reader interface {
	ReadUint8() (uint8, error)
	ReadUint() (int, error)
	ReadString() (string, error)
	ReadValue() (interface{}, error)
}

type ValueReader interface {
	ReadValue() (interface{}, error)
}

// Note: This code is intentionally very verbose/repetitive in order to be forward compatible.

const (
	codeValue uint8 = iota
	codeCopy
	codeBlank

	codeReturnIntoArray
	codeReturnIntoObject
	codeReturnIntoObjectSameKey

	codePushField
	codePushElement
	codePushParent
	codePop

	codePushFieldCopy
	codePushFieldBlank
	codePushElementCopy
	codePushElementBlank

	codeReturnIntoObjectPop
	codeReturnIntoObjectSameKeyPop
	codeReturnIntoArrayPop

	codeObjectSetFieldValue
	codeObjectCopyField
	codeObjectDeleteField

	codeArrayAppendValue
	codeArrayAppendSlice

	codeStringAppendString
	codeStringAppendSlice
)

// Reads a single operation from a reader.
func ReadFrom(r Reader) (Op, error) {
	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	var op Op
	switch code {
	case codeValue:
		op = &OpValue{}
	case codeCopy:
		op = &OpCopy{}
	case codeBlank:
		op = &OpBlank{}
	case codeReturnIntoArray:
		op = &OpReturnIntoArray{}
	case codeReturnIntoObject:
		op = &OpReturnIntoObject{}
	case codeReturnIntoObjectSameKey:
		op = &OpReturnIntoObjectSameKey{}
	case codePushField:
		op = &OpPushField{}
	case codePushElement:
		op = &OpPushElement{}
	case codePushParent:
		op = &OpPushParent{}
	case codePop:
		op = &OpPop{}
	case codePushFieldCopy:
		op = &OpPushFieldCopy{}
	case codePushFieldBlank:
		op = &OpPushFieldBlank{}
	case codePushElementCopy:
		op = &OpPushElementCopy{}
	case codePushElementBlank:
		op = &OpPushElementBlank{}
	case codeReturnIntoObjectPop:
		op = &OpReturnIntoObjectPop{}
	case codeReturnIntoObjectSameKeyPop:
		op = &OpReturnIntoObjectSameKeyPop{}
	case codeReturnIntoArrayPop:
		op = &OpReturnIntoArrayPop{}
	case codeObjectSetFieldValue:
		op = &OpObjectSetFieldValue{}
	case codeObjectCopyField:
		op = &OpObjectCopyField{}
	case codeObjectDeleteField:
		op = &OpObjectDeleteField{}
	case codeArrayAppendValue:
		op = &OpArrayAppendValue{}
	case codeArrayAppendSlice:
		op = &OpArrayAppendSlice{}
	case codeStringAppendString:
		op = &OpStringAppendString{}
	case codeStringAppendSlice:
		op = &OpStringAppendSlice{}
	default:
		return nil, fmt.Errorf("unknown opcode: %d", code)
	}

	err = op.readParams(r)
	if err != nil {
		return nil, err
	}
	return op, nil
}

// Writes a single operation to a writer.
func WriteTo(w Writer, op Op) error {
	var code uint8

	switch op.(type) {
	case *OpValue:
		code = codeValue
	case *OpCopy:
		code = codeCopy
	case *OpBlank:
		code = codeBlank
	case *OpReturnIntoArray:
		code = codeReturnIntoArray
	case *OpReturnIntoObject:
		code = codeReturnIntoObject
	case *OpReturnIntoObjectSameKey:
		code = codeReturnIntoObjectSameKey
	case *OpPushField:
		code = codePushField
	case *OpPushElement:
		code = codePushElement
	case *OpPushParent:
		code = codePushParent
	case *OpPop:
		code = codePop
	case *OpPushFieldCopy:
		code = codePushFieldCopy
	case *OpPushFieldBlank:
		code = codePushFieldBlank
	case *OpPushElementCopy:
		code = codePushElementCopy
	case *OpPushElementBlank:
		code = codePushElementBlank
	case *OpReturnIntoObjectPop:
		code = codeReturnIntoObjectPop
	case *OpReturnIntoObjectSameKeyPop:
		code = codeReturnIntoObjectSameKeyPop
	case *OpReturnIntoArrayPop:
		code = codeReturnIntoArrayPop
	case *OpObjectSetFieldValue:
		code = codeObjectSetFieldValue
	case *OpObjectCopyField:
		code = codeObjectCopyField
	case *OpObjectDeleteField:
		code = codeObjectDeleteField
	case *OpArrayAppendValue:
		code = codeArrayAppendValue
	case *OpArrayAppendSlice:
		code = codeArrayAppendSlice
	case *OpStringAppendString:
		code = codeStringAppendString
	case *OpStringAppendSlice:
		code = codeStringAppendSlice
	}

	err := w.WriteUint8(code)
	if err != nil {
		return err
	}

	err = op.writeParams(w)
	if err != nil {
		return err
	}

	return nil
}

// Writes a patch to a writer.
func (patch Patch) WriteTo(w Writer) error {
	for _, op := range patch {
		err := WriteTo(w, op)
		if err != nil {
			return err
		}
	}

	return nil
}

func (patch *Patch) ReadFrom(r Reader) error {
	*patch = Patch{}

	for {
		op, err := ReadFrom(r)
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		*patch = append(*patch, op)
	}

	return nil
}

func ReadUint8FromValueReader(r ValueReader) (uint8, error) {
	num, err := ReadUintFromValueReader(r)
	if err != nil {
		return 0, err
	}

	if num >= 256 {
		return 0, fmt.Errorf("expected uint8")
	}

	return uint8(num), nil
}

func ReadUintFromValueReader(r ValueReader) (int, error) {
	val, err := r.ReadValue()
	if err != nil {
		return 0, err
	}

	parseAsFloat64 := func(val interface{}) (int, error) {
		res, ok := val.(float64)
		if !ok {
			return 0, fmt.Errorf("expected float64")
		}

		intVal, fracVal := math.Modf(res)
		if fracVal != 0 {
			return 0, fmt.Errorf("expected float64 as integer")
		}

		if intVal < 0 {
			return 0, fmt.Errorf("expected float64 as positive integer")
		}

		return int(intVal), nil
	}

	parseAsInt := func(val interface{}) (int, error) {
		res, ok := val.(int)
		if !ok {
			return 0, fmt.Errorf("expected int")
		}
		return res, nil
	}

	// Try parsing as float64 first (default number type on JSON marshalling). If it fails, try
	// parsing as int (default type for integer numbers in Go).
	v, err1 := parseAsFloat64(val)
	if err1 == nil {
		return v, nil
	}

	v, err2 := parseAsInt(val)
	if err2 == nil {
		return v, nil
	}

	return 0, fmt.Errorf("%s or %s", err1, err2)
}

func ReadStringFromValueReader(r ValueReader) (string, error) {
	val, err := r.ReadValue()
	if err != nil {
		return "", err
	}

	res, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("expected string")
	}

	return res, nil
}

// Implementations:

func (op *OpValue) readParams(r Reader) (err error) {
	op.Value, err = r.ReadValue()
	return
}

func (op *OpValue) writeParams(w Writer) (err error) {
	err = w.WriteValue(op.Value)
	return
}

func (op *OpCopy) readParams(r Reader) (err error) {
	return
}

func (op *OpCopy) writeParams(w Writer) (err error) {
	return
}

func (op *OpBlank) readParams(r Reader) (err error) {
	return
}

func (op *OpBlank) writeParams(w Writer) (err error) {
	return
}

func (op *OpReturnIntoObject) readParams(r Reader) (err error) {
	op.Key, err = r.ReadString()
	return
}

func (op *OpReturnIntoObject) writeParams(w Writer) (err error) {
	err = w.WriteString(op.Key)
	return
}

func (op *OpReturnIntoObjectSameKey) readParams(r Reader) (err error) {
	return
}

func (op *OpReturnIntoObjectSameKey) writeParams(w Writer) (err error) {
	return
}

func (op *OpReturnIntoArray) readParams(r Reader) (err error) {
	return
}

func (op *OpReturnIntoArray) writeParams(w Writer) (err error) {
	return
}

func (op *OpPushField) readParams(r Reader) (err error) {
	op.Index, err = r.ReadUint()
	return
}

func (op *OpPushField) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.Index)
	return
}

func (op *OpPushElement) readParams(r Reader) (err error) {
	op.Index, err = r.ReadUint()
	return
}

func (op *OpPushElement) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.Index)
	return
}

func (op *OpPushParent) readParams(r Reader) (err error) {
	op.N, err = r.ReadUint()
	return
}

func (op *OpPushParent) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.N)
	return
}

func (op *OpPop) readParams(r Reader) (err error) {
	return
}

func (op *OpPop) writeParams(w Writer) (err error) {
	return
}

// Note: all of these helpers don't invoke OpCopy/Blank/Pop because we now they're empty.

func (op *OpPushFieldCopy) readParams(r Reader) (err error) {
	err = op.OpPushField.readParams(r)
	return
}

func (op *OpPushFieldCopy) writeParams(w Writer) (err error) {
	err = op.OpPushField.writeParams(w)
	return
}

func (op *OpPushFieldBlank) readParams(r Reader) (err error) {
	err = op.OpPushField.readParams(r)
	return
}

func (op *OpPushFieldBlank) writeParams(w Writer) (err error) {
	err = op.OpPushField.writeParams(w)
	return
}

func (op *OpPushElementCopy) readParams(r Reader) (err error) {
	err = op.OpPushElement.readParams(r)
	return
}

func (op *OpPushElementCopy) writeParams(w Writer) (err error) {
	err = op.OpPushElement.writeParams(w)
	return
}

func (op *OpPushElementBlank) readParams(r Reader) (err error) {
	err = op.OpPushElement.readParams(r)
	return
}

func (op *OpPushElementBlank) writeParams(w Writer) (err error) {
	err = op.OpPushElement.writeParams(w)
	return
}

func (op *OpReturnIntoObjectPop) readParams(r Reader) (err error) {
	err = op.OpReturnIntoObject.readParams(r)
	return
}

func (op *OpReturnIntoObjectPop) writeParams(w Writer) (err error) {
	err = op.OpReturnIntoObject.writeParams(w)
	return
}

func (op *OpReturnIntoObjectSameKeyPop) readParams(r Reader) (err error) {
	return
}

func (op *OpReturnIntoObjectSameKeyPop) writeParams(w Writer) (err error) {
	return
}

func (op *OpReturnIntoArrayPop) readParams(r Reader) (err error) {
	err = op.OpReturnIntoArray.readParams(r)
	return
}

func (op *OpReturnIntoArrayPop) writeParams(w Writer) (err error) {
	err = op.OpReturnIntoArray.writeParams(w)
	return
}

func (op *OpObjectSetFieldValue) readParams(r Reader) (err error) {
	err = op.OpValue.readParams(r)
	if err != nil {
		return
	}
	err = op.OpReturnIntoObject.readParams(r)
	return
}

func (op *OpObjectSetFieldValue) writeParams(w Writer) (err error) {
	err = op.OpValue.writeParams(w)
	if err != nil {
		return
	}
	err = op.OpReturnIntoObject.writeParams(w)
	return
}

func (op *OpObjectCopyField) readParams(r Reader) (err error) {
	err = op.OpPushField.readParams(r)
	if err != nil {
		return
	}
	return
}

func (op *OpObjectCopyField) writeParams(w Writer) (err error) {
	err = op.OpPushField.writeParams(w)
	if err != nil {
		return
	}
	return
}

func (op *OpObjectDeleteField) readParams(r Reader) (err error) {
	op.Index, err = r.ReadUint()
	return
}

func (op *OpObjectDeleteField) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.Index)
	return
}

func (op *OpArrayAppendValue) readParams(r Reader) (err error) {
	op.Value, err = r.ReadValue()
	return
}

func (op *OpArrayAppendValue) writeParams(w Writer) (err error) {
	err = w.WriteValue(op.Value)
	return
}

func (op *OpArrayAppendSlice) readParams(r Reader) (err error) {
	op.Left, err = r.ReadUint()
	if err != nil {
		return
	}
	op.Right, err = r.ReadUint()
	return
}

func (op *OpArrayAppendSlice) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.Left)
	if err != nil {
		return
	}
	err = w.WriteUint(op.Right)
	return
}

func (op *OpStringAppendString) readParams(r Reader) (err error) {
	op.String, err = r.ReadString()
	return
}

func (op *OpStringAppendString) writeParams(w Writer) (err error) {
	err = w.WriteString(op.String)
	return
}

func (op *OpStringAppendSlice) readParams(r Reader) (err error) {
	op.Left, err = r.ReadUint()
	if err != nil {
		return
	}
	op.Right, err = r.ReadUint()
	return
}

func (op *OpStringAppendSlice) writeParams(w Writer) (err error) {
	err = w.WriteUint(op.Left)
	if err != nil {
		return
	}
	err = w.WriteUint(op.Right)
	return
}
