package mendoza

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"math"
)

// 64-bit ought to be enough
type Hash [8]byte

type Hasher struct {
	hasher hash.Hash
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

func hasherFor(t byte) *Hasher {
	h := &Hasher{
		hasher: sha256.New(),
	}
	h.hasher.Write([]byte{t})
	return h
}


func hashFor(t byte) Hash {
	return hasherFor(t).Sum()
}

var HashTrue = hashFor(typeTrue)
var HashFalse = hashFor(typeFalse)
var HashNull = hashFor(typeNull)

func HashString(s string) Hash {
	h := hasherFor(typeString)
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

func HasherMap() MapHasher {
	return hasherFor(typeMap)
}

func HasherSlice() SliceHasher {
	return hasherFor(typeSlice)
}

func (h *Hasher) Sum() (result Hash) {
	fullSum := h.hasher.Sum(nil)
	copy(result[:], fullSum)
	return result
}

func (h *Hasher) WriteField(key string, value Hash) {
	h.hasher.Write([]byte{typeString})
	h.hasher.Write([]byte(key))
	h.hasher.Write(value[:])
}

func (h *Hasher) WriteElement(value Hash) {
	h.hasher.Write(value[:])
}

