package mendoza_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/sanity-io/mendoza"
)

type benchCase struct {
	name       string
	doc        []byte // the document as stored
	patch      []byte
	patchValue mendoza.Patch
	docValue   interface{}
}

// benchCases are Portable Text articles of 5, 100 and 2,000 blocks (about
// 2 KB, 40 KB and 800 KB), edited as in Studio: a word typed into a span and
// _updatedAt bumped. Each is stored as Go writes it, keys sorted, and as
// Postgres writes jsonb, keys by length.
func benchCases(b *testing.B) []benchCase {
	var cases []benchCase
	for _, blocks := range []int{5, 100, 2000} {
		left := genDoc(blocks)
		right := editDoc(left)
		patch, err := mendoza.CreatePatch(left, right)
		if err != nil {
			b.Fatal(err)
		}
		patchJSON, _ := json.Marshal(patch)
		for _, s := range styles[:2] {
			doc := encode(nil, left, s, rand.New(rand.NewSource(1)))
			cases = append(cases, benchCase{
				name:       fmt.Sprintf("%s/%dKB", s.name, len(doc)>>10),
				doc:        doc,
				patch:      patchJSON,
				patchValue: patch,
				docValue:   left,
			})
		}
	}
	return cases
}

// BenchmarkApply is bytes in, bytes out: what a caller holding the document
// and patch as stored pays.
func BenchmarkApply(b *testing.B) {
	for _, c := range benchCases(b) {
		c := c
		b.Run("decode-apply-encode/"+c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.doc)))
			for i := 0; i < b.N; i++ {
				var doc interface{}
				if err := json.Unmarshal(c.doc, &doc); err != nil {
					b.Fatal(err)
				}
				var patch mendoza.Patch
				if err := json.Unmarshal(c.patch, &patch); err != nil {
					b.Fatal(err)
				}
				if _, err := json.Marshal(mendoza.ApplyPatch(doc, patch)); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("raw/"+c.name, func(b *testing.B) {
			var p mendoza.RawPatcher
			var dst []byte
			b.ReportAllocs()
			b.SetBytes(int64(len(c.doc)))
			for i := 0; i < b.N; i++ {
				var err error
				if dst, err = p.Apply(dst[:0], c.doc, c.patch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkApplyDecoded is ApplyPatch alone, on a decoded document and
// patch: the floor for any caller of ApplyPatch, before it decodes or
// encodes anything.
func BenchmarkApplyDecoded(b *testing.B) {
	for _, c := range benchCases(b) {
		c := c
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.doc)))
			for i := 0; i < b.N; i++ {
				mendoza.ApplyPatch(c.docValue, c.patchValue)
			}
		})
	}
}

// BenchmarkApplyChain rebuilds a version 20 edits away, as history does:
// decode once and apply every patch, against applying each patch to bytes.
func BenchmarkApplyChain(b *testing.B) {
	const n = 20
	for _, blocks := range []int{5, 100, 2000} {
		docs := []map[string]interface{}{genDoc(blocks)}
		patches := make([][]byte, n)
		for i := range patches {
			next := editDoc(docs[i])
			patch, _ := mendoza.CreatePatch(docs[i], next)
			patches[i], _ = json.Marshal(patch)
			docs = append(docs, next)
		}
		doc, _ := json.Marshal(docs[0])
		name := fmt.Sprintf("%dKB", len(doc)>>10)

		b.Run("decode-apply-encode/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var v interface{}
				if err := json.Unmarshal(doc, &v); err != nil {
					b.Fatal(err)
				}
				for _, pj := range patches {
					var patch mendoza.Patch
					if err := json.Unmarshal(pj, &patch); err != nil {
						b.Fatal(err)
					}
					v = mendoza.ApplyPatch(v, patch)
				}
				if _, err := json.Marshal(v); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("raw/"+name, func(b *testing.B) {
			var p mendoza.RawPatcher
			var cur, next []byte
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cur = append(cur[:0], doc...)
				for _, pj := range patches {
					var err error
					if next, err = p.Apply(next[:0], cur, pj); err != nil {
						b.Fatal(err)
					}
					cur, next = next, cur
				}
			}
		})
	}
}
