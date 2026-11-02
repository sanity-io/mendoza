package mendoza

import (
	"encoding/json"
	"fmt"
	"strings"
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

// keylessSpanPair creates documents whose blocks have three spans without _key, and edits one span per block.
// Every span then differs from every other only by its text, so a span's xor candidates are all spans with the same
// parent, but would be every span in the document without keying the xor index by parent.
func keylessSpanPair(n int) (interface{}, interface{}) {
	doc := func(edit bool) interface{} {
		blocks := make([]interface{}, n)
		for i := range blocks {
			spans := make([]interface{}, 3)
			for j := range spans {
				text := fmt.Sprintf("text %d %d", i, j)
				if edit && j == 1 {
					text += "!"
				}
				spans[j] = map[string]interface{}{"_type": "span", "marks": []interface{}{}, "text": text}
			}
			blocks[i] = map[string]interface{}{"_type": "block", "_key": fmt.Sprint(i), "children": spans}
		}
		return norm(map[string]interface{}{"body": blocks})
	}
	return doc(false), doc(true)
}

// replacementPair has no reusable string prefixes or suffixes. Reconstruction loses to
// literal replacement, exercising the cost and memory usage of rejected patches.
func replacementPair(n int) (interface{}, interface{}) {
	left, right := make([]interface{}, n), make([]interface{}, n)
	for i := range left {
		left[i] = strings.Repeat("a", 80) + fmt.Sprint(i) + "x"
		right[i] = strings.Repeat("b", 80) + fmt.Sprint(i) + "y"
	}
	return left, right
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
		d := &Differ{}
		_, _ = d.CreatePatch(left, right) // grow the buffers
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
		d := &Differ{}
		w := &discardWriter{}
		_ = d.Diff(left, right) // grow the buffers
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

func BenchmarkDifferUnchanged(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		left, right := makeDoc(n, false), makeDoc(n, false)
		for _, double := range []bool{false, true} {
			b.Run(fmt.Sprintf("double=%t/%d", double, n), func(b *testing.B) {
				d := &Differ{}
				diff := d.Diff
				if double {
					diff = d.DiffDouble
				}
				if err := diff(left, right); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := diff(left, right); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkDifferScaling checks that diff time grows roughly linearly with document size. Every block repeats values
// such as "_type": "block", so a search for aliases across the whole document would make this quadratic.
func BenchmarkDifferScaling(b *testing.B) {
	for _, n := range []int{1000, 4000, 8000} {
		left, right := heavyPair(n)
		benchDifferScaling(b, fmt.Sprint(n), left, right)
	}
	for _, n := range []int{1000, 4000, 8000} {
		left, right := keylessSpanPair(n)
		benchDifferScaling(b, fmt.Sprintf("keyless/%d", n), left, right)
	}
}

func benchDifferScaling(b *testing.B, name string, left, right interface{}) {
	b.Run(name, func(b *testing.B) {
		d := &Differ{}
		w := &discardWriter{}
		if err := d.Diff(left, right); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
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

// BenchmarkDifferSmallAfterLarge measures steady-state small diffs after a large diff has grown the arenas.
// Exclude the first small diff: releasing references to the large documents once is expected, but subsequent
// small diffs should not keep clearing the unused capacity of those arenas.
func BenchmarkDifferSmallAfterLarge(b *testing.B) {
	for _, n := range []int{0, 1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			d := &Differ{}
			if n > 0 {
				left, right := make([]interface{}, n), make([]interface{}, n)
				for i := range left {
					left[i], right[i] = float64(i), float64(i+n)
				}
				if err := d.Diff(left, right); err != nil {
					b.Fatal(err)
				}
			}
			if err := d.Diff("a", "b"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := d.Diff("a", "b"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkReplacement reports bytes as well as allocation counts: rejected candidates
// should not accumulate on the tape even though the resulting patch is just one literal.
func BenchmarkReplacement(b *testing.B) {
	for _, n := range []int{100, 10000} {
		left, right := replacementPair(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.Run("CreatePatch", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := CreatePatch(left, right); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Differ", func(b *testing.B) {
				d := &Differ{}
				if err := d.Diff(left, right); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := d.Diff(left, right); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkNilPatch(b *testing.B) {
	for _, test := range []struct {
		name        string
		left, right interface{}
	}{
		{"empty", nil, nil},
		{"insert", nil, "insert"},
		{"delete", "delete", nil},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.Run("CreatePatch", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := CreatePatch(test.left, test.right); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("CreateDoublePatch", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, _, err := CreateDoublePatch(test.left, test.right); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
