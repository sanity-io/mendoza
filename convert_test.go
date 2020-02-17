package mendoza_test

import (
	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
	"testing"
)

type CustomObject struct {
	attrs map[string]interface{}
}

func TestConvertObject(t *testing.T) {
	opts := mendoza.DefaultOptions.WithConvertFunc(func(value interface{}) interface{} {
		if value, ok := value.(CustomObject); ok {
			return value.attrs
		}
		if value, ok := value.(CustomArray); ok {
			return value.values
		}
		return value
	})

	customLeft := CustomObject{
		attrs: map[string]interface{}{
			"a": "abcdefgh",
		},
	}

	customRight := CustomObject{
		attrs: map[string]interface{}{
			"a": "abcdefgh",
			"b": 123.0,
		},
	}

	t.Run("TopLevel", func(t *testing.T) {
		left := customLeft
		right := customRight
		result := customRight.attrs

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight := opts.ApplyPatch(left, patch)
		require.EqualValues(t, result, newRight)
	})

	t.Run("Nested", func(t *testing.T) {
		left := map[string]interface{}{"a": customLeft}
		right := map[string]interface{}{"a": customRight}
		result := map[string]interface{}{"a": customRight.attrs}

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight := opts.ApplyPatch(left, patch)
		require.EqualValues(t, result, newRight)
	})
}

type CustomArray struct {
	values []interface{}
}

func TestConvertArray(t *testing.T) {
	opts := mendoza.DefaultOptions.WithConvertFunc(func(value interface{}) interface{} {
		if value, ok := value.(CustomArray); ok {
			return value.values
		}
		return value
	})

	customLeft := CustomArray{
		[]interface{}{map[string]interface{}{
			"a": "abcdefgh",
		}},
	}

	customRight := CustomArray{
		[]interface{}{map[string]interface{}{
			"a": "abcdefgh",
			"b": 123.0,
		}},
	}

	t.Run("TopLevel", func(t *testing.T) {
		left := customLeft
		right := customRight
		result := customRight.values

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight := opts.ApplyPatch(left, patch)
		require.EqualValues(t, result, newRight)
	})

	t.Run("Nested", func(t *testing.T) {
		left := map[string]interface{}{"a": customLeft}
		right := map[string]interface{}{"a": customRight}
		result := map[string]interface{}{"a": customRight.values}

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight := opts.ApplyPatch(left, patch)
		require.EqualValues(t, result, newRight)
	})
}
