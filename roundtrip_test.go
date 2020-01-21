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
}

func TestRoundtrip(t *testing.T) {
	for idx, pair := range Documents {
		t.Run(fmt.Sprintf("N%d", idx), func(t *testing.T) {
			var left, right interface{}

			err := json.Unmarshal([]byte(pair.Left), &left)
			require.NoError(t, err)

			err = json.Unmarshal([]byte(pair.Right), &right)
			require.NoError(t, err)

			patch1, patch2, err := mendoza.DoubleDiff(left, right)
			require.NoError(t, err)

			result1 := mendoza.Decode(left, patch1)
			require.EqualValues(t, right, result1)

			result2 := mendoza.Decode(right, patch2)
			require.EqualValues(t, left, result2)
		})
	}
}
