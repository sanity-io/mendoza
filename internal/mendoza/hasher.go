package mendoza

import (
	"encoding/binary"
	"github.com/sanity-io/mendoza/internal/sha256"
	"math"
)

// 64-bit ought to be enough
type Hash [sha256.Size]byte

type Hasher struct {
	hasher sha256.Digest
}

func (h *Hash) Xor(other Hash) {
	for i, b := range other {
		h[i] ^= b
	}
}

func (h Hash) IsNull() bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

type MapHasher interface {
	WriteField(key string, value Hash)
	Sum() Hash
}

type SliceHasher interface {
	WriteElement(value Hash)
	Sum() Hash
}

const (
	typeString byte = iota
	typeFloat
	typeMap
	typeSlice
	typeTrue
	typeFalse
	typeNull
)

func hasherFor(t byte) Hasher {
	h := Hasher{
		hasher: *sha256.New(),
	}
	h.hasher.Write([]byte{t})
	return h
}

func hashFor(t byte) Hash {
	h := hasherFor(t)
	return h.Sum()
}

var HashTrue = hashFor(typeTrue)
var HashFalse = hashFor(typeFalse)
var HashNull = hashFor(typeNull)
var HasherString = hasherFor(typeString)
var HasherFloat = hasherFor(typeFloat)
var HasherMap = hasherFor(typeMap)
var HasherSlice = hasherFor(typeSlice)

func HashString(s string) Hash {
	h := HasherString
	h.hasher.Write([]byte(s))
	return h.Sum()
}

func HashFloat64(f float64) Hash {
	h := hasherFor(typeFloat)
	bits := math.Float64bits(f)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], bits)
	h.hasher.Write(buf[:])
	return h.Sum()
}

func (h *Hasher) Sum() Hash {
	return h.hasher.CheckSum()
}

func (h *Hasher) WriteField(key string, value Hash) {
	h.hasher.Write([]byte{typeString})
	h.hasher.Write([]byte(key))
	h.hasher.Write(value[:])
}

func (h *Hasher) WriteElement(value Hash) {
	h.hasher.Write(value[:])
}
