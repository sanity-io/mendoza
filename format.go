package mendoza

import "fmt"

// Writer is an interface for writing values. This can be used for supporting a custom serialization format.
type Writer interface {
	WriteUint8(v uint8) error
	WriteUint(v int) error
	WriteString(v string) error
	WriteValue(v interface{}) error
}

// Writer is an interface for reading values. This can be used for supporting a custom serialization format.
type Reader interface {
	ReadUint8() (uint8, error)
	ReadUint() (int, error)
	ReadString() (string, error)
	ReadValue() (interface{}, error)
}

// Note: This code is intentionally very verbose/repetitive in order to be forward compatible.

const (
	codeEnterValue uint8 = iota

	codeEnterRootNop
	codeEnterRootCopy
	codeEnterRootBlank

	codeEnterFieldNop
	codeEnterFieldCopy
	codeEnterFieldBlank

	codeEnterElementNop
	codeEnterElementCopy
	codeEnterElementBlank

	codeReturnIntoArray
	codeReturnIntoObject

	codeObjectSetFieldValue
	codeObjectCopyField
	codeObjectDeleteField

	codeArrayAppendValue
	codeArrayAppendSlice // Index-variant?
)

// Reads a single operation.
func ReadFrom(r Reader) (Op, error) {
	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	switch code {
	case codeEnterValue:
		val, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return OpEnterValue{val}, nil
	case codeEnterRootNop:
		return OpEnterRoot{EnterNop}, nil
	case codeEnterRootCopy:
		return OpEnterRoot{EnterCopy}, nil
	case codeEnterRootBlank:
		return OpEnterRoot{EnterBlank}, nil
	case codeEnterFieldNop:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterNop, idx}, nil
	case codeEnterFieldCopy:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterCopy, idx}, nil
	case codeEnterFieldBlank:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterBlank, idx}, nil
	case codeEnterElementNop:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterNop, idx}, nil
	case codeEnterElementCopy:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterCopy, idx}, nil
	case codeEnterElementBlank:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterBlank, idx}, nil
	case codeReturnIntoArray:
		return OpReturnIntoArray{}, nil
	case codeReturnIntoObject:
		key, err := r.ReadString()
		if err != nil {
			return nil, err
		}
		return OpReturnIntoObject{key}, nil
	case codeObjectSetFieldValue:
		key, err := r.ReadString()
		if err != nil {
			return nil, err
		}
		value, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return  OpObjectSetFieldValue{key, value}, nil
	case codeObjectCopyField:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return  OpObjectCopyField{idx}, nil
	case codeObjectDeleteField:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return  OpObjectDeleteField{idx}, nil
	case codeArrayAppendValue:
		value, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return  OpArrayAppendValue{value}, nil
	case codeArrayAppendSlice:
		left, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		right, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return  OpArrayAppendSlice{left, right}, nil
	default:
		return nil, fmt.Errorf("unknown code: %d", code)
	}
}

// Writes a single operation to a writer.
func WriteTo(w Writer, op Op) error {
	switch op := op.(type) {
	case OpEnterValue:
		err := w.WriteUint8(codeEnterValue)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	case OpEnterRoot:
		switch op.Enter {
		case EnterNop:
			return w.WriteUint8(codeEnterRootNop)
		case EnterCopy:
			return w.WriteUint8(codeEnterRootCopy)
		case EnterBlank:
			return w.WriteUint8(codeEnterRootBlank)
		default:
			panic("invalid enter type")
		}
	case OpEnterField:
		switch op.Enter {
		case EnterNop:
			err := w.WriteUint8(codeEnterFieldNop)
			if err != nil {
				return err
			}
		case EnterCopy:
			err := w.WriteUint8(codeEnterFieldCopy)
			if err != nil {
				return err
			}
		case EnterBlank:
			err := w.WriteUint8(codeEnterFieldBlank)
			if err != nil {
				return err
			}
		default:
			panic("invalid enter type")
		}

		return w.WriteUint(op.Index)
	case OpEnterElement:
		switch op.Enter {
		case EnterNop:
			err := w.WriteUint8(codeEnterElementNop)
			if err != nil {
				return err
			}
		case EnterCopy:
			err := w.WriteUint8(codeEnterElementCopy)
			if err != nil {
				return err
			}
		case EnterBlank:
			err := w.WriteUint8(codeEnterElementBlank)
			if err != nil {
				return err
			}
		default:
			panic("invalid enter type")
		}

		return w.WriteUint(op.Index)
	case OpReturnIntoArray:
		return w.WriteUint8(codeReturnIntoArray)
	case OpReturnIntoObject:
		err := w.WriteUint8(codeReturnIntoObject)
		if err != nil {
			return err
		}
		return w.WriteString(op.Key)
	case OpObjectSetFieldValue:
		err := w.WriteUint8(codeObjectSetFieldValue)
		if err != nil {
			return err
		}
		err = w.WriteString(op.Key)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	case OpObjectCopyField:
		err := w.WriteUint8(codeObjectCopyField)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Index)
	case OpObjectDeleteField:
		err := w.WriteUint8(codeObjectDeleteField)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Index)
	case OpArrayAppendSlice:
		err := w.WriteUint8(codeArrayAppendSlice)
		if err != nil {
			return err
		}
		err = w.WriteUint(op.Left)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Right)
	case OpArrayAppendValue:
		err := w.WriteUint8(codeArrayAppendValue)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	}

	panic("unknown op")
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