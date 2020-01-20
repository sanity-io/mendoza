package mendoza

import (
	"encoding/json"
	"fmt"
)

const (
	CodeReturn = EnterFinal + iota
	CodeOutputValue
	CodeSetFieldValue
	CodeCopyField
	CodeDeleteField
	CodeAppendValue
	CodeAppendSlice
)

type jsonWriter struct {
	result []byte
	err    error
}

func (w *jsonWriter) next() {
	if len(w.result) == 0 {
		w.result = append(w.result, '[')
	} else {
		w.result = append(w.result, ',')
	}
}

func (w *jsonWriter) writeValue(value interface{}) {
	w.next()
	b, err := json.Marshal(value)
	if err != nil {
		w.err = err
		return
	}
	w.result = append(w.result, b...)
}

func (w *jsonWriter) finalize() []byte {
	if len(w.result) == 0 {
		return []byte{'[', ']'}
	}

	w.result = append(w.result, ']')
	return w.result
}

func (patch Patch) MarshalJSON() ([]byte, error) {
	w := jsonWriter{}

	for _, op := range patch {
		switch op := op.(type) {
		case OpOutputValue:
			w.writeValue(CodeOutputValue)
			w.writeValue(op.Value)
		case OpEnterRoot:
			w.writeValue(op.Enter)
		case OpEnterField:
			w.writeValue(op.Enter)
			w.writeValue(op.Key)
		case OpEnterElement:
			w.writeValue(op.Enter)
			w.writeValue(op.Index)
		case OpReturn:
			w.writeValue(CodeReturn)
			if len(op.Key) > 0 {
				w.writeValue(op.Key)
			}
		case OpSetFieldValue:
			w.writeValue(CodeSetFieldValue)
			w.writeValue(op.Key)
			w.writeValue(op.Value)
		case OpCopyField:
			w.writeValue(CodeCopyField)
			w.writeValue(op.Key)
		case OpDeleteField:
			w.writeValue(CodeDeleteField)
			w.writeValue(op.Key)
		case OpAppendValue:
			w.writeValue(CodeAppendValue)
			w.writeValue(op.Value)
		case OpAppendSlice:
			w.writeValue(CodeAppendSlice)
			w.writeValue(op.Left)
			w.writeValue(op.Right)
		default:
			panic(fmt.Errorf("unknown op: %#v", op))
		}
	}

	return w.finalize(), nil
}
