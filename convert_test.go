package mendoza_test

import (
	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
	"testing"
)

type Custom struct {
	attrs map[string]interface{}
}

func TestConvert(t *testing.T) {
	opts := mendoza.DefaultOptions.WithConvertFunc(func(value interface{}) interface{} {
		if value, ok := value.(Custom); ok {
			return value.attrs
		}
		return value
	})

	left := Custom{
		attrs: map[string]interface{}{
			"a": "abcdefgh",
		},
	}

	right := Custom{
		attrs: map[string]interface{}{
			"a": "abcdefgh",
			"b": 123.0,
		},
	}

	patch, err := opts.CreatePatch(left, right)
	require.NoError(t, err)

	newRight := opts.ApplyPatch(left, patch)
	require.EqualValues(t, map[string]interface{}{
		"a": "abcdefgh",
		"b": 123.0,
	}, newRight)
}
