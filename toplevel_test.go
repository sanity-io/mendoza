package mendoza_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
)

// TestTopLevelCreatePatch exercises the package-level mendoza.CreatePatch
// wrapper (differ.go:18), which only delegates to DefaultOptions but is
// itself separately measured for coverage.
func TestTopLevelCreatePatch(t *testing.T) {
	pairs := []struct {
		Left  string
		Right string
	}{
		{`{"a":"a","b":"b"}`, `{"a":"a","b":"c"}`},
		{`{"k":["a","b","c"]}`, `{"k":["a","b","c","d"]}`},
		{`"hello world"`, `"hello there"`},
		{`[1,2,3]`, `[1,2,3,4]`},
		{`{"nested":{"x":1,"y":2}}`, `{"nested":{"x":1,"y":3}}`},
	}

	for idx, pair := range pairs {
		t.Run(fmt.Sprintf("N%d", idx), func(t *testing.T) {
			var left, right interface{}
			require.NoError(t, json.Unmarshal([]byte(pair.Left), &left))
			require.NoError(t, json.Unmarshal([]byte(pair.Right), &right))

			patch, err := mendoza.CreatePatch(left, right)
			require.NoError(t, err)

			result := mendoza.ApplyPatch(left, patch)
			require.EqualValues(t, right, result)
		})
	}
}

// TestCreatePatchNilLeft exercises the left==nil branch in
// (*Options).CreatePatch (differ.go:32-37) via the top-level wrapper.
func TestCreatePatchNilLeft(t *testing.T) {
	t.Run("BothNil", func(t *testing.T) {
		patch, err := mendoza.CreatePatch(nil, nil)
		require.NoError(t, err)
		require.Len(t, patch, 0)
		// Applying an empty patch to nil should yield nil.
		result := mendoza.ApplyPatch(nil, patch)
		require.Nil(t, result)
	})

	t.Run("NilLeftConcreteRight", func(t *testing.T) {
		right := map[string]interface{}{"a": "b"}
		patch, err := mendoza.CreatePatch(nil, right)
		require.NoError(t, err)
		// Must be a single OpValue op carrying the right document.
		require.Len(t, patch, 1)

		result := mendoza.ApplyPatch(nil, patch)
		require.EqualValues(t, right, result)
	})

	t.Run("NilLeftStringRight", func(t *testing.T) {
		patch, err := mendoza.CreatePatch(nil, "hello")
		require.NoError(t, err)
		result := mendoza.ApplyPatch(nil, patch)
		require.EqualValues(t, "hello", result)
	})
}

// TestCreateDoublePatchNilBranches exercises the three nil-handling
// branches in (*Options).CreateDoublePatch (differ.go:60-70).
func TestCreateDoublePatchNilBranches(t *testing.T) {
	t.Run("BothNil", func(t *testing.T) {
		p1, p2, err := mendoza.CreateDoublePatch(nil, nil)
		require.NoError(t, err)
		require.Len(t, p1, 0)
		require.Len(t, p2, 0)
		require.Nil(t, mendoza.ApplyPatch(nil, p1))
		require.Nil(t, mendoza.ApplyPatch(nil, p2))
	})

	t.Run("LeftNil", func(t *testing.T) {
		right := []interface{}{"a", "b", "c"}
		p1, p2, err := mendoza.CreateDoublePatch(nil, right)
		require.NoError(t, err)
		require.EqualValues(t, right, mendoza.ApplyPatch(nil, p1))
		require.Nil(t, mendoza.ApplyPatch(right, p2))
	})

	t.Run("RightNil", func(t *testing.T) {
		left := map[string]interface{}{"a": "b", "c": "d"}
		p1, p2, err := mendoza.CreateDoublePatch(left, nil)
		require.NoError(t, err)
		require.Nil(t, mendoza.ApplyPatch(left, p1))
		require.EqualValues(t, left, mendoza.ApplyPatch(nil, p2))
	})
}

// TestMaybeApplyPatchSuccess exercises the happy path of the top-level
// mendoza.MaybeApplyPatch wrapper (patcher.go:36).
func TestMaybeApplyPatchSuccess(t *testing.T) {
	left := map[string]interface{}{
		"a": "alpha",
		"b": "beta",
	}
	right := map[string]interface{}{
		"a": "alpha",
		"b": "gamma",
		"c": "delta",
	}

	patch, err := mendoza.CreatePatch(left, right)
	require.NoError(t, err)

	result, err := mendoza.MaybeApplyPatch(left, patch)
	require.NoError(t, err)
	require.EqualValues(t, right, result)
}

// TestMaybeApplyPatchEmptyPatch exercises the early-return branch in
// MaybeApplyPatch when the patch is empty (patcher.go:51-53). It should
// return the input document unchanged with no error.
func TestMaybeApplyPatchEmptyPatch(t *testing.T) {
	doc := map[string]interface{}{"unchanged": true}
	result, err := mendoza.MaybeApplyPatch(doc, mendoza.Patch{})
	require.NoError(t, err)
	require.EqualValues(t, doc, result)
}

// TestMaybeApplyPatchError exercises the error path in MaybeApplyPatch:
// an op whose applyTo returns ErrInvalidPatch should surface as an error
// (and a nil result) without panicking. We feed OpObjectDeleteField with
// a positive index against an empty array — inputEntry().getField will
// fail the map type assertion and return ErrInvalidPatch.
func TestMaybeApplyPatchError(t *testing.T) {
	badPatch := mendoza.Patch{&mendoza.OpObjectDeleteField{Index: 0}}
	result, err := mendoza.MaybeApplyPatch([]interface{}{}, badPatch)
	require.Error(t, err)
	require.Equal(t, mendoza.ErrInvalidPatch, err)
	require.Nil(t, result)
}
