package mendoza

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

type jsonWriter struct {
	result []byte
	err    error
}

func (w *jsonWriter) WriteUint8(v uint8) error {
	return w.WriteValue(v)
}

func (w *jsonWriter) WriteUint(v int) error {
	return w.WriteValue(v)
}

func (w *jsonWriter) WriteString(v string) error {
	return w.WriteValue(v)
}

func (w *jsonWriter) WriteValue(v interface{}) error {
	w.next()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.result = append(w.result, b...)
	return nil
}

func (w *jsonWriter) next() {
	if len(w.result) == 0 {
		w.result = append(w.result, '[')
	} else {
		w.result = append(w.result, ',')
	}
}

func (w *jsonWriter) finalize() []byte {
	if len(w.result) == 0 {
		return []byte{'[', ']'}
	}

	w.result = append(w.result, ']')
	return w.result
}

type jsonReader struct {
	dec *json.Decoder
}

func (r *jsonReader) tryEof() error {
	if !r.dec.More() {
		t, err := r.dec.Token()
		if err != nil {
			return err
		}
		if t != json.Delim(']') {
			return fmt.Errorf("expected ] at end")
		}

		return io.EOF
	}

	return nil
}

func (r *jsonReader) ReadUint8() (uint8, error) {
	num, err := r.ReadUint()
	if err != nil {
		return 0, err
	}

	if num >= 256 {
		return 0, fmt.Errorf("expected uint8")
	}

	return uint8(num), nil
}

func (r *jsonReader) ReadUint() (int, error) {
	val, err := r.ReadValue()
	if err != nil {
		return 0, err
	}

	res, ok := val.(float64)
	if !ok {
		return 0, fmt.Errorf("expected number")
	}

	intVal, fracVal := math.Modf(res)
	if fracVal != 0 {
		return 0, fmt.Errorf("expected integer")
	}

	if intVal < 0 {
		return 0, fmt.Errorf("expected positive integer")
	}

	return int(intVal), nil
}

func (r *jsonReader) ReadString() (string, error) {
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

func (r *jsonReader) ReadValue() (interface{}, error) {
	err := r.tryEof()
	if err != nil {
		return nil, err
	}
	var val interface{}
	err = r.dec.Decode(&val)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (r *jsonReader) expectArray() error {
	t, err := r.dec.Token()
	if err != nil {
		return err
	}

	if t != json.Delim('[') {
		return fmt.Errorf("expected array")
	}

	return nil
}


func (patch Patch) MarshalJSON() ([]byte, error) {
	w := jsonWriter{}
	err := patch.WriteTo(&w)
	if err != nil {
		return nil, err
	}
	return w.finalize(), nil
}

func (patch *Patch) UnmarshalJSON(data []byte) error {
	r := jsonReader{
		dec: json.NewDecoder(bytes.NewReader(data)),
	}

	err := r.expectArray()
	if err != nil {
		return err
	}

	*patch = Patch{}

	for {
		op, err := ReadFrom(&r)
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
