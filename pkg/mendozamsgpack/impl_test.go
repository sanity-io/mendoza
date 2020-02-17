package mendozamsgpack_test

import (
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
