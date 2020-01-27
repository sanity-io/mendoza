package mendoza

import "fmt"

type Writer interface {
	WriteUint8(v uint8) error
	WriteUint(v int) error
	WriteString(v string) error
	WriteValue(v interface{}) error
}

type Reader interface {
	ReadUint8() (uint8, error)
	ReadUint() (int, error)
	ReadString() (string, error)
	ReadValue() (interface{}, error)
}

// Note: This code is intentionally very verbose/repetitive in order to be forward compatible.

const (
	CodeEnterValue uint8 = iota

	CodeEnterRootNop
	CodeEnterRootCopy
	CodeEnterRootBlank

	CodeEnterFieldNop
	CodeEnterFieldCopy
	CodeEnterFieldBlank

	CodeEnterElementNop
	CodeEnterElementCopy
	CodeEnterElementBlank

	CodeReturnIntoArray
	CodeReturnIntoObject // Key-less variant?

	CodeObjectSetFieldValue
	CodeObjectCopyField
	CodeObjectDeleteField

	CodeArrayAppendValue
	CodeArrayAppendSlice // Index-variant?
)

func ReadFrom(r Reader) (Op, error) {
	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	switch code {
	case CodeEnterValue:
		val, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return OpEnterValue{val}, nil
	case CodeEnterRootNop:
		return OpEnterRoot{EnterNop}, nil
	case CodeEnterRootCopy:
		return OpEnterRoot{EnterCopy}, nil
	case CodeEnterRootBlank:
		return OpEnterRoot{EnterBlank}, nil
	case CodeEnterFieldNop:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterNop, idx}, nil
	case CodeEnterFieldCopy:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterCopy, idx}, nil
	case CodeEnterFieldBlank:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterField{EnterBlank, idx}, nil
	case CodeEnterElementNop:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterNop, idx}, nil
	case CodeEnterElementCopy:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterCopy, idx}, nil
	case CodeEnterElementBlank:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return OpEnterElement{EnterBlank, idx}, nil
	case CodeReturnIntoArray:
		return OpReturnIntoArray{}, nil
	case CodeReturnIntoObject:
		key, err := r.ReadString()
		if err != nil {
			return nil, err
		}
		return OpReturnIntoObject{key}, nil
	case CodeObjectSetFieldValue:
		key, err := r.ReadString()
		if err != nil {
			return nil, err
		}
		value, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return  OpObjectSetFieldValue{key, value}, nil
	case CodeObjectCopyField:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return  OpObjectCopyField{idx}, nil
	case CodeObjectDeleteField:
		idx, err := r.ReadUint()
		if err != nil {
			return nil, err
		}
		return  OpObjectDeleteField{idx}, nil
	case CodeArrayAppendValue:
		value, err := r.ReadValue()
		if err != nil {
			return nil, err
		}
		return  OpArrayAppendValue{value}, nil
	case CodeArrayAppendSlice:
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

func WriteTo(w Writer, op Op) error {
	switch op := op.(type) {
	case OpEnterValue:
		err := w.WriteUint8(CodeEnterValue)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	case OpEnterRoot:
		switch op.Enter {
		case EnterNop:
			return w.WriteUint8(CodeEnterRootNop)
		case EnterCopy:
			return w.WriteUint8(CodeEnterRootCopy)
		case EnterBlank:
			return w.WriteUint8(CodeEnterRootBlank)
		default:
			panic("invalid enter type")
		}
	case OpEnterField:
		switch op.Enter {
		case EnterNop:
			err := w.WriteUint8(CodeEnterFieldNop)
			if err != nil {
				return err
			}
		case EnterCopy:
			err := w.WriteUint8(CodeEnterFieldCopy)
			if err != nil {
				return err
			}
		case EnterBlank:
			err := w.WriteUint8(CodeEnterFieldBlank)
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
			err := w.WriteUint8(CodeEnterElementNop)
			if err != nil {
				return err
			}
		case EnterCopy:
			err := w.WriteUint8(CodeEnterElementCopy)
			if err != nil {
				return err
			}
		case EnterBlank:
			err := w.WriteUint8(CodeEnterElementBlank)
			if err != nil {
				return err
			}
		default:
			panic("invalid enter type")
		}

		return w.WriteUint(op.Index)
	case OpReturnIntoArray:
		return w.WriteUint8(CodeReturnIntoArray)
	case OpReturnIntoObject:
		err := w.WriteUint8(CodeReturnIntoObject)
		if err != nil {
			return err
		}
		return w.WriteString(op.Key)
	case OpObjectSetFieldValue:
		err := w.WriteUint8(CodeObjectSetFieldValue)
		if err != nil {
			return err
		}
		err = w.WriteString(op.Key)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	case OpObjectCopyField:
		err := w.WriteUint8(CodeObjectCopyField)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Index)
	case OpObjectDeleteField:
		err := w.WriteUint8(CodeObjectDeleteField)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Index)
	case OpArrayAppendSlice:
		err := w.WriteUint8(CodeArrayAppendSlice)
		if err != nil {
			return err
		}
		err = w.WriteUint(op.Left)
		if err != nil {
			return err
		}
		return w.WriteUint(op.Right)
	case OpArrayAppendValue:
		err := w.WriteUint8(CodeArrayAppendValue)
		if err != nil {
			return err
		}
		return w.WriteValue(op.Value)
	}

	panic("unknown op")
}

func (patch Patch) WriteTo(w Writer) error {
	for _, op := range patch {
		err := WriteTo(w, op)
		if err != nil {
			return err
		}
	}

	return nil
}