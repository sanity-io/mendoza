package mendoza

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	return ReadUint8FromValueReader(r)
}

func (r *jsonReader) ReadUint() (int, error) {
	return ReadUintFromValueReader(r)
}

func (r *jsonReader) ReadString() (string, error) {
	return ReadStringFromValueReader(r)
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

type jsonValueReader struct {
	data []interface{}
	idx int
}

func (r *jsonValueReader) ReadUint8() (uint8, error) {
	return ReadUint8FromValueReader(r)
}

func (r *jsonValueReader) ReadUint() (int, error) {
	return ReadUintFromValueReader(r)
}

func (r *jsonValueReader) ReadString() (string, error) {
	return ReadStringFromValueReader(r)
}

func (r *jsonValueReader) ReadValue() (interface{}, error) {
	if r.idx >= len(r.data) {
		return nil, io.EOF
	}
	idx := r.idx
	r.idx++
	return r.data[idx], nil
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

	return patch.ReadFrom(&r)
}

// DecodeJSON decodes a patch from an []interface{} as parsed by encoding/json.
func (patch *Patch) DecodeJSON(data []interface{}) error {
	r := jsonValueReader{data: data}
	return patch.ReadFrom(&r)
}
