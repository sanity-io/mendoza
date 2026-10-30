package mendoza

import (
	"encoding/binary"
	"hash/maphash"
	"math"
)

// Hash is a 128-bit content hash built from two independently seeded maphash
// values. Hashes are never persisted or sent over the wire, so a per-process
// random seed is fine and makes precomputed collisions infeasible.
type Hash [16]byte

func (h *Hash) Xor(other Hash) {
	a := binary.LittleEndian.Uint64(h[:8]) ^ binary.LittleEndian.Uint64(other[:8])
	b := binary.LittleEndian.Uint64(h[8:]) ^ binary.LittleEndian.Uint64(other[8:])
	binary.LittleEndian.PutUint64(h[:8], a)
	binary.LittleEndian.PutUint64(h[8:], b)
}

func (h Hash) IsNull() bool { return h == Hash{} }

// Words returns the hash as two 64-bit words.
func (h Hash) Words() (uint64, uint64) {
	return binary.LittleEndian.Uint64(h[:8]), binary.LittleEndian.Uint64(h[8:])
}

// SetWords sets the hash from two 64-bit words.
func (h *Hash) SetWords(a, b uint64) {
	binary.LittleEndian.PutUint64(h[:8], a)
	binary.LittleEndian.PutUint64(h[8:], b)
}

func mk(a, b uint64) (h Hash) {
	binary.LittleEndian.PutUint64(h[:8], a)
	binary.LittleEndian.PutUint64(h[8:], b)
	return
}

type seedPair struct{ a, b maphash.Seed }

func newSeeds() seedPair { return seedPair{maphash.MakeSeed(), maphash.MakeSeed()} }

var (
	seedString = newSeeds()
	seedFloat  = newSeeds()
	seedMap    = newSeeds()
	seedSlice  = newSeeds()
	seedConst  = newSeeds()
)

var HashTrue = mk(maphash.String(seedConst.a, "true"), maphash.String(seedConst.b, "true"))
var HashFalse = mk(maphash.String(seedConst.a, "false"), maphash.String(seedConst.b, "false"))
var HashNull = mk(maphash.String(seedConst.a, "null"), maphash.String(seedConst.b, "null"))

func HashString(s string) Hash {
	return mk(maphash.String(seedString.a, s), maphash.String(seedString.b, s))
}

func HashFloat64(f float64) Hash {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
	return mk(maphash.Bytes(seedFloat.a, buf[:]), maphash.Bytes(seedFloat.b, buf[:]))
}

// Hasher hashes a container incrementally. It is a value type and does not allocate.
type Hasher struct{ a, b maphash.Hash }

func newHasher(s seedPair) (h Hasher) {
	h.a.SetSeed(s.a)
	h.b.SetSeed(s.b)
	return
}

func NewMapHasher() Hasher   { return newHasher(seedMap) }
func NewSliceHasher() Hasher { return newHasher(seedSlice) }

func (h *Hasher) write(p []byte) { h.a.Write(p); h.b.Write(p) }

func (h *Hasher) WriteField(key string, value Hash) {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(key))) // length-prefix: unambiguous framing
	h.write(n[:])
	h.a.WriteString(key)
	h.b.WriteString(key)
	h.write(value[:])
}

func (h *Hasher) WriteElement(value Hash) { h.write(value[:]) }

func (h *Hasher) Sum() Hash { return mk(h.a.Sum64(), h.b.Sum64()) }
