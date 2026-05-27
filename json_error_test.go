package mendoza_test

import (
	"encoding/json"
	"testing"

	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
)

// TestUnmarshalJSONExpectArrayError exercises the expectArray error branch
// in Patch.UnmarshalJSON (json.go:99-110). A JSON document whose top-level
// value is not an array must surface an "expected array" error.
func TestUnmarshalJSONExpectArrayError(t *testing.T) {
	var patch mendoza.Patch
	err := json.Unmarshal([]byte(`{"not":"array"}`), &patch)
	require.Error(t, err)
}

// TestUnmarshalJSONExpectArrayTokenError exercises expectArray's
// Token()-returns-error branch (json.go:101-103) via wholly malformed JSON.
func TestUnmarshalJSONExpectArrayTokenError(t *testing.T) {
	var patch mendoza.Patch
	err := json.Unmarshal([]byte(`@@@not-json@@@`), &patch)
	require.Error(t, err)
}

// TestUnmarshalJSONUnknownOpcode exercises the unknown-opcode branch in
// ReadFrom (format.go:122-124) via the JSON wire codec.
func TestUnmarshalJSONUnknownOpcode(t *testing.T) {
	var patch mendoza.Patch
	// 255 is not a defined opcode.
	err := json.Unmarshal([]byte(`[255]`), &patch)
	require.Error(t, err)
}

// TestUnmarshalJSONTryEofMissingBracket exercises tryEof's "expected ]"
// branch (json.go:64-66): a JSON array that is not closed properly after
// a successful op decode should error out.
func TestUnmarshalJSONTryEofMissingBracket(t *testing.T) {
	var patch mendoza.Patch
	// codePop == 9 takes no params. After it, the decoder should hit ']'
	// but instead finds another token. We deliberately use a malformed
	// array (extra token after the op).
	err := json.Unmarshal([]byte(`[9 9]`), &patch)
	require.Error(t, err)
}

// TestUnmarshalJSONEmptyArray exercises the happy ']' close in tryEof
// (json.go:60-68): an empty JSON array is a valid (empty) patch.
func TestUnmarshalJSONEmptyArray(t *testing.T) {
	var patch mendoza.Patch
	err := json.Unmarshal([]byte(`[]`), &patch)
	require.NoError(t, err)
	require.Len(t, patch, 0)
}

// TestUnmarshalJSONInvalidValue exercises the dec.Decode error branch in
// jsonReader.ReadValue (json.go:92-95) by giving an OpValue op (code 0)
// followed by an invalid JSON value.
func TestUnmarshalJSONInvalidValue(t *testing.T) {
	var patch mendoza.Patch
	// `[0, @@@]` -- opcode 0 = OpValue, then a value that's invalid JSON.
	err := json.Unmarshal([]byte(`[0, @@@]`), &patch)
	require.Error(t, err)
}

// TestDecodeJSONWrongOpcodeType exercises ReadUint8FromValueReader's
// num>=256 branch (format.go:238-240) via DecodeJSON with a number too
// large to be a uint8 opcode.
func TestDecodeJSONWrongOpcodeType(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{float64(99999)})
	require.Error(t, err)
}

// TestDecodeJSONFractionalOpcode exercises ReadUintFromValueReader's
// "fracVal != 0" branch (format.go:255-257). A fractional float as opcode
// is rejected.
func TestDecodeJSONFractionalOpcode(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{float64(1.5)})
	require.Error(t, err)
}

// TestDecodeJSONNegativeOpcode exercises the negative-float branch
// (format.go:259-261) in ReadUintFromValueReader.
func TestDecodeJSONNegativeOpcode(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{float64(-1)})
	require.Error(t, err)
}

// TestDecodeJSONIntOpcode exercises the int-typed branch in
// ReadUintFromValueReader (format.go:263-267). The decoder accepts a raw
// Go int (matching int branch) as an opcode, which is what we feed via
// DecodeJSON's []interface{} entry-point.
func TestDecodeJSONIntOpcode(t *testing.T) {
	var patch mendoza.Patch
	// codePop is 9 and takes no params.
	err := patch.DecodeJSON([]interface{}{int(9)})
	require.NoError(t, err)
	require.Len(t, patch, 1)
}

// TestDecodeJSONNegativeIntOpcode exercises the negative-int branch
// (format.go:264-266).
func TestDecodeJSONNegativeIntOpcode(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{int(-3)})
	require.Error(t, err)
}

// TestDecodeJSONWrongOpcodeTypeString exercises the default branch
// (format.go:268-270) of ReadUintFromValueReader: a string where a uint
// is expected.
func TestDecodeJSONWrongOpcodeTypeString(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{"not-a-number"})
	require.Error(t, err)
}

// TestDecodeJSONWrongStringType exercises ReadStringFromValueReader's
// type-assertion failure branch (format.go:279-282). codeReturnIntoObject
// is 4 and reads a string param; we feed it a number instead.
func TestDecodeJSONWrongStringType(t *testing.T) {
	var patch mendoza.Patch
	// opcode 4 = codeReturnIntoObject (reads one string).
	err := patch.DecodeJSON([]interface{}{float64(4), float64(42)})
	require.Error(t, err)
}

// TestDecodeJSONStringReadEOF exercises ReadStringFromValueReader's
// "read fails" branch via a truncated patch: opcode wants a string param
// but the slice is already exhausted. The inner io.EOF propagates back
// up to Patch.ReadFrom which treats it as the end-of-stream signal, so
// no error is reported — but the read code path is still exercised, and
// the resulting patch is empty (because the partially-decoded op was
// abandoned by the loop's EOF branch).
func TestDecodeJSONStringReadEOF(t *testing.T) {
	var patch mendoza.Patch
	// opcode 4 = codeReturnIntoObject expects a string param; provide none.
	err := patch.DecodeJSON([]interface{}{float64(4)})
	require.NoError(t, err)
	require.Len(t, patch, 0)
}

// TestDecodeJSONUintReadEOF exercises ReadUintFromValueReader's EOF branch
// (format.go:247-249): opcode requires a uint param but none follows.
// Same end-of-stream behavior as above — error is swallowed by ReadFrom.
func TestDecodeJSONUintReadEOF(t *testing.T) {
	var patch mendoza.Patch
	// opcode 6 = codePushField expects one uint param; provide none.
	err := patch.DecodeJSON([]interface{}{float64(6)})
	require.NoError(t, err)
	require.Len(t, patch, 0)
}

// TestDecodeJSONValueReadEOF exercises jsonValueReader.ReadValue's EOF
// branch (json.go:130-132) — opcode 0 = OpValue needs a value param.
// Same end-of-stream behavior.
func TestDecodeJSONValueReadEOF(t *testing.T) {
	var patch mendoza.Patch
	err := patch.DecodeJSON([]interface{}{float64(0)})
	require.NoError(t, err)
	require.Len(t, patch, 0)
}

// TestMarshalUnmarshalEmptyPatch round-trips an empty patch through JSON
// to exercise the "no result yet" branch of jsonWriter.finalize
// (json.go:46-48): result starts empty, so finalize returns "[]".
func TestMarshalUnmarshalEmptyPatch(t *testing.T) {
	empty := mendoza.Patch{}
	data, err := json.Marshal(empty)
	require.NoError(t, err)
	require.Equal(t, "[]", string(data))

	var back mendoza.Patch
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back, 0)
}
