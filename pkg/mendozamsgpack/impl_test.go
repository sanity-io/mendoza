package mendozamsgpack_test

import (
	"github.com/sanity-io/mendoza"
	"github.com/sanity-io/mendoza/pkg/mendozamsgpack"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEncodingSize(t *testing.T) {
	patch := mendoza.Patch{
		mendoza.OpEnterRoot{mendoza.EnterBlank},
		mendoza.OpArrayAppendSlice{0, 6},
	}

	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)
	require.Len(t, b, 6)
}

func TestRoundtrip(t *testing.T) {
	// This patch isn't valid
	patch := mendoza.Patch{
		mendoza.OpEnterRoot{mendoza.EnterBlank},
		mendoza.OpEnterField{mendoza.EnterCopy, 10},
		mendoza.OpEnterElement{mendoza.EnterNop, 1000000},
		mendoza.OpEnterValue{"abc"},
		mendoza.OpArrayAppendSlice{0, 6},
	}

	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)

	decodedPatch, err := mendozamsgpack.Unmarshal(b)
	require.NoError(t, err)

	require.EqualValues(t, patch, decodedPatch)
}

func TestEmptyPatch(t *testing.T) {
	patch := mendoza.Patch{}
	b, err := mendozamsgpack.Marshal(patch)
	require.NoError(t, err)
	require.NotNil(t, b)
}
