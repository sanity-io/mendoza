package mendozamsgpack

import (
	"github.com/sanity-io/mendoza"
	"github.com/vmihailenco/msgpack/v4"
	"io"
)

// MsgpackPatch is an alias for mendoza.Patch which implements CustomEncoder/CustomDecoder.
// You should only use this if you need to embed a patch inside a larger msgpack structure.
// Otherwise it's preferred to use the Marshal and Unmarshal functions.
type MsgpackPatch mendoza.Patch

var _ msgpack.CustomEncoder = (*MsgpackPatch)(nil)
var _ msgpack.CustomDecoder = (*MsgpackPatch)(nil)

// Marshal encodes a Mendoza patch using Msgpack.
func Marshal(patch mendoza.Patch) ([]byte, error) {
	mppatch := MsgpackPatch(patch)
	return msgpack.Marshal(&mppatch)
}

// Marshal decodes a Mendoza patch using Msgpack.
func Unmarshal(data []byte) (mendoza.Patch, error) {
	var mppatch MsgpackPatch
	err := msgpack.Unmarshal(data, &mppatch)
	if err != nil {
		return nil, err
	}
	return mendoza.Patch(mppatch), nil
}


type writer struct {
	*msgpack.Encoder
}

func (w writer) WriteUint8(v uint8) error {
	return w.EncodeUint8(v)
}

func (w writer) WriteUint(v int) error {
	return w.EncodeUint(uint64(v))
}

func (w writer) WriteString(v string) error {
	return w.EncodeString(v)
}

func (w writer) WriteValue(v interface{}) error {
	return w.Encode(v)
}

func (patch *MsgpackPatch) EncodeMsgpack(enc *msgpack.Encoder) error {
	w := writer{enc}
	for _, op := range *patch {
		err := mendoza.WriteTo(w, op)
		if err != nil {
			return err
		}
	}

	return nil
}

type reader struct {
	*msgpack.Decoder
}

func (r reader) ReadUint8() (uint8, error) {
	return r.DecodeUint8()
}

func (r reader) ReadUint() (int, error) {
	val, err := r.DecodeUint()
	return int(val), err
}

func (r reader) ReadString() (string, error) {
	return r.DecodeString()
}

func (r reader) ReadValue() (interface{}, error) {
	var result interface{}
	err := r.Decode(&result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (patch *MsgpackPatch) DecodeMsgpack(dec *msgpack.Decoder) error {
	r := reader{dec}

	for {
		op, err := mendoza.ReadFrom(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		*patch = append(*patch, op)
	}
}
