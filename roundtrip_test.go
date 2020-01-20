package mendoza_test

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/litter"
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

			patch, err := mendoza.Diff(left, right)
			require.NoError(t, err)

			litter.Dump(patch)

			result := mendoza.Decode(left, patch)
			require.EqualValues(t, right, result)
		})
	}
}
