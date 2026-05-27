package mendoza

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHashIsNullZero verifies that the zero Hash value is considered null.
func TestHashIsNullZero(t *testing.T) {
	var h Hash
	require.True(t, h.IsNull())
}

// TestHashIsNullNonZero verifies that any computed hash is non-null.
func TestHashIsNullNonZero(t *testing.T) {
	require.False(t, HashTrue.IsNull())
	require.False(t, HashFalse.IsNull())
	require.False(t, HashNull.IsNull())
	require.False(t, HashString("anything").IsNull())
	require.False(t, HashFloat64(0).IsNull())
}

// TestHashXorSelf verifies that XOR with itself yields the zero hash.
func TestHashXorSelf(t *testing.T) {
	h := HashString("xyz")
	h.Xor(HashString("xyz"))
	require.True(t, h.IsNull())
}

// TestHashXorRoundtrip verifies XOR is reversible.
func TestHashXorRoundtrip(t *testing.T) {
	a := HashString("a")
	b := HashString("b")

	combined := a
	combined.Xor(b)
	combined.Xor(b)

	require.Equal(t, a, combined)
}

// TestHashStringDistinct verifies HashString yields distinct hashes for distinct inputs.
func TestHashStringDistinct(t *testing.T) {
	require.NotEqual(t, HashString("a"), HashString("b"))
	// And deterministic.
	require.Equal(t, HashString("hello"), HashString("hello"))
}

// TestHashFloat64Distinct verifies HashFloat64 yields distinct hashes for distinct values.
func TestHashFloat64Distinct(t *testing.T) {
	require.NotEqual(t, HashFloat64(0), HashFloat64(1))
	require.NotEqual(t, HashFloat64(1.5), HashFloat64(-1.5))
	require.Equal(t, HashFloat64(3.14), HashFloat64(3.14))
}

// TestHashListForScalars exercises HashListFor on every supported scalar type
// (nil, bool, float64, string).
func TestHashListForScalars(t *testing.T) {
	cases := []interface{}{
		nil,
		true,
		false,
		float64(0),
		float64(-12.5),
		float64(1e10),
		"",
		"hello",
	}
	for _, c := range cases {
		hl, err := HashListFor(c, nil)
		require.NoError(t, err)
		require.Len(t, hl.Entries, 1)
		require.True(t, hl.Entries[0].Size >= 1)
	}
}

// TestHashListForCompound exercises HashListFor on maps and slices, including
// empty and nested forms.
func TestHashListForCompound(t *testing.T) {
	t.Run("EmptyMap", func(t *testing.T) {
		hl, err := HashListFor(map[string]interface{}{}, nil)
		require.NoError(t, err)
		require.Len(t, hl.Entries, 1)
		require.False(t, hl.Entries[0].IsNonEmptyMap())
		require.False(t, hl.Entries[0].IsNonEmptySlice())
	})

	t.Run("EmptySlice", func(t *testing.T) {
		hl, err := HashListFor([]interface{}{}, nil)
		require.NoError(t, err)
		require.Len(t, hl.Entries, 1)
		require.False(t, hl.Entries[0].IsNonEmptySlice())
		require.False(t, hl.Entries[0].IsNonEmptyMap())
	})

	t.Run("NonEmptyMap", func(t *testing.T) {
		hl, err := HashListFor(map[string]interface{}{
			"a": "alpha",
			"b": float64(2),
		}, nil)
		require.NoError(t, err)
		require.Len(t, hl.Entries, 3)
		require.True(t, hl.Entries[0].IsNonEmptyMap())
	})

	t.Run("NonEmptySlice", func(t *testing.T) {
		hl, err := HashListFor([]interface{}{
			"first",
			"second",
			"third",
		}, nil)
		require.NoError(t, err)
		require.Len(t, hl.Entries, 4)
		require.True(t, hl.Entries[0].IsNonEmptySlice())
	})

	t.Run("Nested", func(t *testing.T) {
		hl, err := HashListFor(map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"k": "v"},
				"plain",
			},
		}, nil)
		require.NoError(t, err)
		require.True(t, len(hl.Entries) >= 4)
		// Two equivalent documents must produce identical root hashes.
		hl2, err := HashListFor(map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"k": "v"},
				"plain",
			},
		}, nil)
		require.NoError(t, err)
		require.Equal(t, hl.Entries[0].Hash, hl2.Entries[0].Hash)
	})
}

// TestHashListForUnsupportedType exercises the error branch in
// HashList.process when given a value of a type it doesn't recognise.
func TestHashListForUnsupportedType(t *testing.T) {
	_, err := HashListFor(int(42), nil)
	require.Error(t, err)
}

// TestHashListForUnsupportedNested ensures the error is surfaced when an
// unsupported type is nested inside a slice.
func TestHashListForUnsupportedNested(t *testing.T) {
	_, err := HashListFor([]interface{}{int(7)}, nil)
	require.Error(t, err)
}

// TestHashListForUnsupportedInMap ensures the error is surfaced from within
// the map branch as well.
func TestHashListForUnsupportedInMap(t *testing.T) {
	_, err := HashListFor(map[string]interface{}{"k": int(9)}, nil)
	require.Error(t, err)
}

// TestHashListForConvertFunc exercises the convertFunc branch of process.
// The convert function rewrites all int values to their float64 equivalents,
// so the resulting hash list builds without error.
func TestHashListForConvertFunc(t *testing.T) {
	convert := func(value interface{}) interface{} {
		if v, ok := value.(int); ok {
			return float64(v)
		}
		return value
	}
	hl, err := HashListFor(map[string]interface{}{
		"a": int(5),
		"b": int(7),
	}, convert)
	require.NoError(t, err)
	require.Len(t, hl.Entries, 3)

	// The same document constructed with native float64s must yield the same
	// root hash as the converted one.
	hl2, err := HashListFor(map[string]interface{}{
		"a": float64(5),
		"b": float64(7),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, hl.Entries[0].Hash, hl2.Entries[0].Hash)
}

// TestHashListIter exercises the (*HashList).Iter helper and the Iter
// navigation methods over a non-trivial map.
func TestHashListIter(t *testing.T) {
	hl, err := HashListFor(map[string]interface{}{
		"a": "x",
		"b": "y",
		"c": "z",
	}, nil)
	require.NoError(t, err)

	// Walk the children of the root via Iter. Should visit 3 entries.
	visited := 0
	for it := hl.Iter(0); !it.IsDone(); it.Next() {
		entry := it.GetEntry()
		require.Equal(t, 0, entry.Parent)
		// Each key should be one of a, b, c.
		key := it.GetKey()
		require.Contains(t, []string{"a", "b", "c"}, key)
		require.True(t, it.GetIndex() > 0)
		visited++
	}
	require.Equal(t, 3, visited)
}

// TestHashListIterSliceChildren exercises Iter over a slice's children.
func TestHashListIterSliceChildren(t *testing.T) {
	hl, err := HashListFor([]interface{}{"a", "b", "c", "d"}, nil)
	require.NoError(t, err)

	visited := 0
	for it := hl.Iter(0); !it.IsDone(); it.Next() {
		entry := it.GetEntry()
		require.Equal(t, 0, entry.Parent)
		visited++
	}
	require.Equal(t, 4, visited)
}

// TestReferenceHelpers verifies the MapEntryReference and SliceEntryReference
// factories produce the expected Reference values.
func TestReferenceHelpers(t *testing.T) {
	mref := MapEntryReference(3, "key")
	require.Equal(t, 3, mref.Index)
	require.Equal(t, "key", mref.Key)

	sref := SliceEntryReference(7)
	require.Equal(t, 7, sref.Index)
	require.Equal(t, "", sref.Key)
}

// TestNewHashIndexLookup verifies NewHashIndex indexes every entry's hash and
// allows finding entries by hash.
func TestNewHashIndexLookup(t *testing.T) {
	hl, err := HashListFor(map[string]interface{}{
		"a": "shared-string",
		"b": "shared-string", // same value, same hash
		"c": "unique",
	}, nil)
	require.NoError(t, err)

	idx := NewHashIndex(hl)
	require.NotNil(t, idx)

	// Shared hash maps to multiple indices.
	shared := HashString("shared-string")
	require.Len(t, idx.Data[shared], 2)

	// Unique hash maps to exactly one.
	unique := HashString("unique")
	require.Len(t, idx.Data[unique], 1)

	// The root entry should also be present in Data.
	require.Contains(t, idx.Data, hl.Entries[0].Hash)
}

// TestNewHashIndexXorData verifies that the XorData index gets populated for
// non-empty compound entries.
func TestNewHashIndexXorData(t *testing.T) {
	hl, err := HashListFor(map[string]interface{}{
		"a": "alpha",
		"b": "beta",
	}, nil)
	require.NoError(t, err)

	idx := NewHashIndex(hl)
	// XorData should be populated since the root map has a non-null XorHash.
	require.NotEmpty(t, idx.XorData)
}
