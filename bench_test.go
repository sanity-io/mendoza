package mendoza

import (
	"encoding/json"
	"fmt"
	"testing"
)

// makeDoc creates a Portable Text-like document with nBlocks paragraphs. If edit is set, the title and one
// paragraph are changed.
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
	return norm(doc).(map[string]interface{})
}

// heavyPair creates two documents where 10% of the paragraphs are edited, and (for n > 4) a paragraph is moved, one
// is deleted and one is inserted.
func heavyPair(n int) (map[string]interface{}, map[string]interface{}) {
	l, r := makeDoc(n, false), makeDoc(n, false)
	body := r["body"].([]interface{})
	for i := 0; i < n; i += 10 {
		ch := body[i].(map[string]interface{})["children"].([]interface{})[0].(map[string]interface{})
		ch["text"] = ch["text"].(string) + " edited"
	}
	if n > 4 {
		body[1], body[3] = body[3], body[1]
		body = append(body[:2], body[3:]...)
		body = append(body, map[string]interface{}{"_type": "block", "_key": "new", "children": []interface{}{}})
		r["body"] = body
	}
	return l, r
}

// norm round-trips v through JSON so that it looks like a decoded document.
func norm(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var o interface{}
	if err := json.Unmarshal(b, &o); err != nil {
		panic(err)
	}
	return o
}

// discardWriter counts what is written to it without allocating.
type discardWriter struct{ n int }

func (w *discardWriter) WriteUint8(v uint8) error       { w.n++; return nil }
func (w *discardWriter) WriteUint(v int) error          { w.n++; return nil }
func (w *discardWriter) WriteString(v string) error     { w.n++; return nil }
func (w *discardWriter) WriteValue(v interface{}) error { w.n++; return nil }

func benchPairs(b *testing.B, f func(b *testing.B, left, right interface{})) {
	for _, n := range []int{10, 100, 1000} {
		left, right := makeDoc(n, false), makeDoc(n, true)
		b.Run(fmt.Sprintf("single/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			f(b, left, right)
		})
	}
	for _, n := range []int{10, 100, 1000} {
		left, right := heavyPair(n)
		b.Run(fmt.Sprintf("heavy/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			f(b, left, right)
		})
	}
}

func BenchmarkCreatePatch(b *testing.B) {
	benchPairs(b, func(b *testing.B, left, right interface{}) {
		for i := 0; i < b.N; i++ {
			if _, err := CreatePatch(left, right); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCreateDoublePatch(b *testing.B) {
	benchPairs(b, func(b *testing.B, left, right interface{}) {
		for i := 0; i < b.N; i++ {
			if _, _, err := CreateDoublePatch(left, right); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDifferCreatePatch(b *testing.B) {
	benchPairs(b, func(b *testing.B, left, right interface{}) {
		d := NewDiffer()
		d.CreatePatch(left, right) // grow the buffers
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := d.CreatePatch(left, right); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDifferWriteTo(b *testing.B) {
	benchPairs(b, func(b *testing.B, left, right interface{}) {
		d := NewDiffer()
		w := &discardWriter{}
		d.Diff(left, right) // grow the buffers
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := d.Diff(left, right); err != nil {
				b.Fatal(err)
			}
			if err := d.WriteTo(w); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkApplyPatch(b *testing.B) {
	benchPairs(b, func(b *testing.B, left, right interface{}) {
		patch, err := CreatePatch(left, right)
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ApplyPatch(left, patch)
		}
	})
}
