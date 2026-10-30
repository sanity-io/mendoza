package mendoza

import (
	"encoding/json"
	"fmt"
	"testing"
)

func makeDoc(nBlocks int, edit bool) map[string]interface{} {
	blocks := []interface{}{}
	for i := 0; i < nBlocks; i++ {
		text := fmt.Sprintf("This is paragraph number %d of a reasonably long portable text body, with some words.", i)
		if edit && i == nBlocks/2 {
			text = fmt.Sprintf("This is paragraph number %d of a reasonably long portable text body, with some EDITED words.", i)
		}
		blocks = append(blocks, map[string]interface{}{
			"_type": "block", "_key": fmt.Sprintf("k%06d", i), "style": "normal",
			"markDefs": []interface{}{},
			"children": []interface{}{
				map[string]interface{}{"_type": "span", "_key": fmt.Sprintf("s%06d", i), "text": text, "marks": []interface{}{"strong"}},
			},
		})
	}
	title := "My article"
	if edit {
		title = "My article (v2)"
	}
	doc := map[string]interface{}{
		"_id": "drafts.abc123", "_type": "article", "_rev": "rev1",
		"title": title, "slug": map[string]interface{}{"_type": "slug", "current": "my-article"},
		"publishedAt": "2026-10-30T10:00:00Z", "views": float64(42), "featured": true,
		"author": map[string]interface{}{"_type": "reference", "_ref": "author-1"},
		"tags":   []interface{}{"go", "json", "diff"},
		"body":   blocks,
	}
	// round-trip through JSON so it looks like real decoded input
	b, _ := json.Marshal(doc)
	var out map[string]interface{}
	json.Unmarshal(b, &out)
	return out
}

var sizes = []int{10, 100, 1000}

func BenchmarkCreatePatch(b *testing.B) {
	for _, n := range sizes {
		l, r := makeDoc(n, false), makeDoc(n, true)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				CreatePatch(l, r)
			}
		})
	}
}

func BenchmarkCreateDoublePatch(b *testing.B) {
	for _, n := range sizes {
		l, r := makeDoc(n, false), makeDoc(n, true)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				CreateDoublePatch(l, r)
			}
		})
	}
}

func BenchmarkApplyPatch(b *testing.B) {
	for _, n := range sizes {
		l, r := makeDoc(n, false), makeDoc(n, true)
		p, _ := CreatePatch(l, r)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ApplyPatch(l, p)
			}
		})
	}
}

func BenchmarkHashList(b *testing.B) {
	for _, n := range sizes {
		l := makeDoc(n, false)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				hashListForBench(l)
			}
		})
	}
}
