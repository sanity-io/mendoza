package mendoza

import (
	"crypto/sha256"
	"fmt"
	"hash/maphash"
	"testing"

	"github.com/sanity-io/mendoza/internal/mendoza"
)

func heavyPair(n int) (map[string]interface{}, map[string]interface{}) {
	l, r := makeDoc(n, false), makeDoc(n, false)
	body := r["body"].([]interface{})
	for i := 0; i < n; i += 10 { // edit 10% of blocks
		ch := body[i].(map[string]interface{})["children"].([]interface{})[0].(map[string]interface{})
		ch["text"] = ch["text"].(string) + " edited"
	}
	if n > 4 { // move a block, delete one, insert one
		body[1], body[3] = body[3], body[1]
		body = append(body[:2], body[3:]...)
		body = append(body, map[string]interface{}{"_type": "block", "_key": "new", "children": []interface{}{}})
		r["body"] = body
	}
	return l, r
}

func BenchmarkHeavyCreatePatch(b *testing.B) {
	for _, n := range sizes {
		l, r := heavyPair(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				CreatePatch(l, r)
			}
		})
	}
}

var str = "This is paragraph number 123 of a reasonably long portable text body, with some words."
var sink [16]byte
var sink64 uint64

func BenchmarkHashStrInternal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ { sink = mendoza.HashString(str) }
}
func BenchmarkHashStrStdlibSHA256(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ { s := sha256.Sum256([]byte(str)); copy(sink[:], s[:16]) }
}
var seed1, seed2 = maphash.MakeSeed(), maphash.MakeSeed()
func BenchmarkHashStrMaphash2x(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ { sink64 = maphash.String(seed1, str) ^ maphash.String(seed2, str) }
}
