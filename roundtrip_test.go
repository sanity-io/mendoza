package mendoza_test

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
	"testing"
)

var Documents = []struct {
	Left  string
	Right string
}{
	{
		`{}`,
		`{}`,
	},
	{
		`1`,
		`{}`,
	},
	{
		`{"a": "b"}`,
		`{"a": "b"}`,
	},
	{
		`{"a": "a"}`,
		`{"a": "b"}`,
	},
	{
		`{"a": "a", "b": "b"}`,
		`{"a": "b"}`,
	},
	{
		`{"a": "a", "b": "b", "c": "c"}`,
		`{"a": "a", "b": "b", "c": "c", "d": "d"}`,
	},
	{
		`{"a": "a", "b": "b", "c": "c"}`,
		`{"d": "d"}`,
	},
	{
		`{"a": "a", "b": {"a": "a"}}`,
		`{"a": "a", "b": {"a": "b", "b": "a"}}`,
	},
	{
		`{"a": ["a", "b", "c"]}`,
		`{"a": ["a", "b", "c"]}`,
	},
	{
		`{"a": ["a", "b", "c"]}`,
		`{"a": ["a", "b"]}`,
	},
	{
		`{"a": [1, 2]}`,
		`{"a": [2, 3]}`,
	},
	{
		`{"a": "abcdef"}`,
		`{"a": "abcdefg"}`,
	},
	{
		`{"a": "abcdef"}`,
		`{"a": "abcgihdef"}`,
	},
	{
		`{"a": "abcdefghijk"}`,
		`{"a": "abcdehijk"}`,
	},
	{
		`{"a": "abcdefghijk"}`,
		`{"a": "bcdeghijk"}`,
	},
	{
		`"abc"`,
		`"abcdef"`,
	},
	{
		`"abc"`,
		`"abc"`,
	},
	{
		`"a:{},:{},"`,
		`"a:{},"`,
	},
	{
		`[[]]`,
		`[]`,
	},
	{
		`{"":""}`,
		`{"":"","0000":""}`,
	},
	{
		`{"H":{"":{}}}`,
		`{"H":0}`,
	},
	{
		`"݆݆݅Ʌ"`,
		`"І݆Ʌ"`,
	},
	// Same-key alias with blank-start: triggers OpObjectCopyField.
	// Left has two fields sharing the same value-hash; right keeps only one
	// (the same-key one), so removeCount == aliasCount and isCopy is false.
	{
		`{"x": "longvaluexxxxxxxxxxx", "y": "longvaluexxxxxxxxxxx"}`,
		`{"y": "longvaluexxxxxxxxxxx"}`,
	},
	// Different-key alias with blank-start: triggers OpReturnIntoObjectPop
	// (via the OpPushFieldCopy + OpReturnIntoObjectPop alias branch).
	{
		`{"x": "longvaluexxxxxxxxxxx"}`,
		`{"y": "longvaluexxxxxxxxxxx"}`,
	},
	// Nested rename inside an array: triggers OpPushElementBlank, because the
	// inner-map reconstruction starts blank and the parent in the left tree
	// is a non-empty slice (so enterBlank emits OpPushElementBlank).
	{
		`{"arr": [{"x": "longvaluexxxxxxxxxxx"}]}`,
		`{"arr": [{"y": "longvaluexxxxxxxxxxx"}]}`,
	},
	// Array of similar objects with one moving position; exercises the
	// slice-alias adjacency logic (OpArrayAppendSlice with non-trivial bounds).
	{
		`[{"k":"aaaaaaaaaaaaa"},{"k":"bbbbbbbbbbbbb"},{"k":"ccccccccccccc"}]`,
		`[{"k":"bbbbbbbbbbbbb"},{"k":"ccccccccccccc"},{"k":"aaaaaaaaaaaaa"}]`,
	},
	// Mixed primitives in arrays: numbers, strings, booleans, nulls, objects.
	{
		`[1, "two", true, null, {"k": 0}]`,
		`[null, false, "two", 2, {"k": 1}]`,
	},
	// Booleans and nulls as object values.
	{
		`{"a": true, "b": false, "c": null, "d": 0}`,
		`{"a": false, "b": true, "c": 1, "d": null}`,
	},
	// Numeric edges: zero, negative, fractional, large.
	{
		`{"a": 0, "b": -1, "c": 1.5, "d": 1000000000000}`,
		`{"a": 1, "b": -2, "c": 2.5, "d": 1000000000001}`,
	},
	// Deep nesting (depth >= 4) with a single leaf change.
	{
		`{"a":{"b":{"c":{"d":{"e":[1,2,3]}}}}}`,
		`{"a":{"b":{"c":{"d":{"e":[1,2,4]}}}}}`,
	},
	// Long string with a shared prefix, changed middle, and shared suffix
	// (>= 64 chars). Triggers both OpStringAppendSlice and OpStringAppendString.
	{
		`{"s": "common-prefix-shared-AAAAAAAAAA-common-suffix-shared-tail-tail-tail"}`,
		`{"s": "common-prefix-shared-BBBBBBBBBB-common-suffix-shared-tail-tail-tail"}`,
	},
	// Object with many (>=20) fields to exercise hash-index reuse.
	{
		`{"f00":0,"f01":1,"f02":2,"f03":3,"f04":4,"f05":5,"f06":6,"f07":7,"f08":8,"f09":9,"f10":10,"f11":11,"f12":12,"f13":13,"f14":14,"f15":15,"f16":16,"f17":17,"f18":18,"f19":19}`,
		`{"f00":0,"f01":1,"f02":2,"f03":3,"f04":4,"f05":5,"f06":6,"f07":7,"f08":8,"f09":9,"f10":10,"f11":11,"f12":12,"f13":13,"f14":14,"f15":15,"f16":16,"f17":17,"f18":18,"f19":99}`,
	},
	// Empty objects swapped with non-empty siblings.
	{
		`{"a": {}, "b": {"x": 1}}`,
		`{"a": {"x": 1}, "b": {}}`,
	},
}

func decodePatch(data []byte, patch *mendoza.Patch) error {
	var value []interface{}
	err := json.Unmarshal(data, &value)
	if err != nil {
		return err
	}
	err = patch.DecodeJSON(value)
	if err != nil {
		return err
	}
	return nil
}

func TestRoundtrip(t *testing.T) {
	for idx, pair := range Documents {
		t.Run(fmt.Sprintf("N%d", idx), func(t *testing.T) {
			var left, right interface{}

			err := json.Unmarshal([]byte(pair.Left), &left)
			require.NoError(t, err)

			err = json.Unmarshal([]byte(pair.Right), &right)
			require.NoError(t, err)

			patch1, patch2, err := mendoza.CreateDoublePatch(left, right)
			require.NoError(t, err)

			result1 := mendoza.ApplyPatch(left, patch1)
			require.EqualValues(t, right, result1)

			result2 := mendoza.ApplyPatch(right, patch2)
			require.EqualValues(t, left, result2)

			// Now try to encode and decode the patch
			json1, err := json.Marshal(patch1)
			require.NoError(t, err)
			var parsedPatch1, decodedPatch1 mendoza.Patch
			err = json.Unmarshal(json1, &parsedPatch1)
			require.NoError(t, err)
			err = decodePatch(json1, &decodedPatch1)
			require.EqualValues(t, patch1, parsedPatch1)
			require.EqualValues(t, parsedPatch1, decodedPatch1)

			json2, err := json.Marshal(patch2)
			require.NoError(t, err)
			var parsedPatch2, decodedPatch2 mendoza.Patch
			err = json.Unmarshal(json2, &parsedPatch2)
			require.NoError(t, err)
			err = decodePatch(json2, &decodedPatch2)
			require.NoError(t, err)
			require.EqualValues(t, patch2, parsedPatch2)
			require.EqualValues(t, parsedPatch2, decodedPatch2)
		})
	}
}
