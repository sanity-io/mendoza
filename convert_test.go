package mendoza_test

import (
	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
	"testing"
)

type CustomObject struct {
	attrs map[string]any
}

func TestConvertObject(t *testing.T) {
	opts := mendoza.DefaultOptions.WithConvertFunc(func(value any) any {
		if value, ok := value.(CustomObject); ok {
			return value.attrs
		}
		if value, ok := value.(CustomArray); ok {
			return value.values
		}
		return value
	})

	customLeft := CustomObject{
		attrs: map[string]any{
			"a": "abcdefgh",
		},
	}

	customRight := CustomObject{
		attrs: map[string]any{
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

		newRight, err := opts.ApplyPatch(left, patch)
		require.NoError(t, err)
		require.EqualValues(t, result, newRight)
	})

	t.Run("Nested", func(t *testing.T) {
		left := map[string]any{"a": customLeft}
		right := map[string]any{"a": customRight}
		result := map[string]any{"a": customRight.attrs}

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight, err := opts.ApplyPatch(left, patch)
		require.NoError(t, err)
		require.EqualValues(t, result, newRight)
	})
}

type CustomArray struct {
	values []any
}

func TestConvertArray(t *testing.T) {
	opts := mendoza.DefaultOptions.WithConvertFunc(func(value any) any {
		if value, ok := value.(CustomArray); ok {
			return value.values
		}
		return value
	})

	customLeft := CustomArray{
		[]any{map[string]any{
			"a": "abcdefgh",
		}},
	}

	customRight := CustomArray{
		[]any{map[string]any{
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

		newRight, err := opts.ApplyPatch(left, patch)
		require.NoError(t, err)
		require.EqualValues(t, result, newRight)
	})

	t.Run("Nested", func(t *testing.T) {
		left := map[string]any{"a": customLeft}
		right := map[string]any{"a": customRight}
		result := map[string]any{"a": customRight.values}

		patch, err := opts.CreatePatch(left, right)
		require.NoError(t, err)

		newRight, err := opts.ApplyPatch(left, patch)
		require.NoError(t, err)
		require.EqualValues(t, result, newRight)
	})
}
