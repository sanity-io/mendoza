package mendozamsgpack_test

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/mendoza"
	"github.com/sanity-io/mendoza/pkg/mendozamsgpack"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEncodingSize(t *testing.T) {
	patch := mendoza.Patch{
		&mendoza.OpBlank{},
		&mendoza.OpArrayAppendSlice{0, 6},
	}

	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)
	require.Len(t, b, 6)
}

func TestRoundtrip(t *testing.T) {
	// This patch isn't valid, we're only testing that it roundtrips properly
	patch := mendoza.Patch{
		&mendoza.OpBlank{},
		&mendoza.OpPushFieldCopy{OpPushField: mendoza.OpPushField{10}},
		&mendoza.OpPushElement{1000000},
		&mendoza.OpValue{"abc"},
		&mendoza.OpArrayAppendSlice{0, 6},
	}

	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)

	decodedPatch, err := mendozamsgpack.Unmarshal(b)
	require.NoError(t, err)

	require.EqualValues(t, patch, decodedPatch)
}

func TestSize(t *testing.T) {
	left := map[string]interface{}{
		"_type": "Person",
		"name": "Bob",
		"age": 10.0,
	}
	right := map[string]interface{}{
		"_type": "Person",
		"name": "Bob",
		"age": 15.0,
	}

	patch, err := mendoza.CreatePatch(left, right)
	require.NoError(t, err)

	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)

	// TODO: We should probably be able to reduce this even further.
	require.True(t, len(b) < 20)
}

func TestEmptyPatch(t *testing.T) {
	patch := mendoza.Patch{}
	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)
	require.NotNil(t, b)
}

// TestBinaryRoundtripDocuments exercises the binary wire format
// (format.go readParams/writeParams for every Op variant) by generating
// real patches from a rich set of document pairs, marshalling them with
// mendozamsgpack (which uses mendoza.WriteTo/ReadFrom internally), and
// confirming both the resulting patch and the applied document match.
func TestBinaryRoundtripDocuments(t *testing.T) {
	pairs := []struct {
		Left  string
		Right string
	}{
		// Object field add/remove/change.
		{`{"a":"a","b":"b","c":"c"}`, `{"a":"a","b":"b","c":"c","d":"d"}`},
		{`{"a":"a","b":"b","c":"c"}`, `{"d":"d"}`},
		// Nested object change.
		{`{"a":"a","b":{"a":"a"}}`, `{"a":"a","b":{"a":"b","b":"a"}}`},
		// Array reordering of similar objects (slice-alias ops).
		{
			`[{"k":"aaaaaaaaaaaaa"},{"k":"bbbbbbbbbbbbb"},{"k":"ccccccccccccc"}]`,
			`[{"k":"bbbbbbbbbbbbb"},{"k":"ccccccccccccc"},{"k":"aaaaaaaaaaaaa"}]`,
		},
		// Long string with shared prefix/suffix (string slice + append ops).
		{
			`{"s":"common-prefix-shared-AAAAAAAAAA-common-suffix-shared-tail-tail-tail"}`,
			`{"s":"common-prefix-shared-BBBBBBBBBB-common-suffix-shared-tail-tail-tail"}`,
		},
		// Same-key alias triggering OpObjectCopyField.
		{
			`{"x":"longvaluexxxxxxxxxxx","y":"longvaluexxxxxxxxxxx"}`,
			`{"y":"longvaluexxxxxxxxxxx"}`,
		},
		// Different-key alias triggering OpReturnIntoObjectPop branch.
		{
			`{"x":"longvaluexxxxxxxxxxx"}`,
			`{"y":"longvaluexxxxxxxxxxx"}`,
		},
		// Nested rename inside array triggering OpPushElementBlank.
		{
			`{"arr":[{"x":"longvaluexxxxxxxxxxx"}]}`,
			`{"arr":[{"y":"longvaluexxxxxxxxxxx"}]}`,
		},
		// Mixed primitives in arrays and nulls/booleans/numbers.
		{`[1,"two",true,null,{"k":0}]`, `[null,false,"two",2,{"k":1}]`},
		{`{"a":true,"b":false,"c":null,"d":0}`, `{"a":false,"b":true,"c":1,"d":null}`},
		// Deep nesting.
		{
			`{"a":{"b":{"c":{"d":{"e":[1,2,3]}}}}}`,
			`{"a":{"b":{"c":{"d":{"e":[1,2,4]}}}}}`,
		},
		// Object with many fields (hash-index reuse).
		{
			`{"f00":0,"f01":1,"f02":2,"f03":3,"f04":4,"f05":5,"f06":6,"f07":7,"f08":8,"f09":9,"f10":10,"f11":11,"f12":12,"f13":13,"f14":14,"f15":15,"f16":16,"f17":17,"f18":18,"f19":19}`,
			`{"f00":0,"f01":1,"f02":2,"f03":3,"f04":4,"f05":5,"f06":6,"f07":7,"f08":8,"f09":9,"f10":10,"f11":11,"f12":12,"f13":13,"f14":14,"f15":15,"f16":16,"f17":17,"f18":18,"f19":99}`,
		},
		// Plain string change.
		{`"abc"`, `"abcdef"`},
	}

	for idx, pair := range pairs {
		t.Run(fmt.Sprintf("N%d", idx), func(t *testing.T) {
			var left, right interface{}
			require.NoError(t, json.Unmarshal([]byte(pair.Left), &left))
			require.NoError(t, json.Unmarshal([]byte(pair.Right), &right))

			patch1, patch2, err := mendoza.CreateDoublePatch(left, right)
			require.NoError(t, err)

			// Forward: marshal then unmarshal patch1, applying must give right.
			b1, err := mendozamsgpack.Marshal(patch1)
			require.NoError(t, err)
			decoded1, err := mendozamsgpack.Unmarshal(b1)
			require.NoError(t, err)
			require.EqualValues(t, patch1, decoded1)
			require.EqualValues(t, right, mendoza.ApplyPatch(left, decoded1))

			// Reverse direction exercises additional op variants in many cases.
			b2, err := mendozamsgpack.Marshal(patch2)
			require.NoError(t, err)
			decoded2, err := mendozamsgpack.Unmarshal(b2)
			require.NoError(t, err)
			require.EqualValues(t, patch2, decoded2)
			require.EqualValues(t, left, mendoza.ApplyPatch(right, decoded2))
		})
	}
}
