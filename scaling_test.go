package mendoza_test

import (
	"fmt"
	"testing"

	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
)

// blockDocument creates a document with n Portable Text-like blocks of three spans each. Every span repeats
// "_type": "span" and "marks": [], so looking up those values across the whole document, instead of under the
// parent being reconstructed, makes diffing quadratic in the number of blocks. If edit is set, one span per block is
// changed.
func blockDocument(n int, keyed bool, edit bool) map[string]interface{} {
	blocks := make([]interface{}, n)
	for i := range blocks {
		spans := make([]interface{}, 3)
		for j := range spans {
			text := fmt.Sprintf("text %d %d", i, j)
			if edit && j == 1 {
				text += "!"
			}
			span := map[string]interface{}{"_type": "span", "marks": []interface{}{}, "text": text}
			if keyed {
				span["_key"] = fmt.Sprintf("s%d-%d", i, j)
			}
			spans[j] = span
		}
		blocks[i] = map[string]interface{}{"_type": "block", "_key": fmt.Sprint(i), "style": "normal", "children": spans}
	}
	return map[string]interface{}{"_type": "article", "body": blocks}
}

func TestBlockDocumentRoundtrip(t *testing.T) {
	for _, keyed := range []bool{false, true} {
		left, right := blockDocument(50, keyed, false), blockDocument(50, keyed, true)
		forward, reverse, err := mendoza.CreateDoublePatch(left, right)
		require.NoError(t, err)
		require.Equal(t, right, mendoza.ApplyPatch(left, forward))
		require.Equal(t, left, mendoza.ApplyPatch(right, reverse))
	}
}

func BenchmarkBlockDocument(b *testing.B) {
	for _, keyed := range []bool{false, true} {
		for _, n := range []int{1000, 4000} {
			left, right := blockDocument(n, keyed, false), blockDocument(n, keyed, true)
			b.Run(fmt.Sprintf("keyed=%t/%d", keyed, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := mendoza.CreatePatch(left, right); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
