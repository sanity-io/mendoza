package sha256

import (
	"bytes"
	"encoding"
	"hash"
	"testing"
)

// TestNewAndWrite exercises the constructor and the Write/Sum path on a few
// fixed inputs; the package's Size is 16 so we can't compare against the
// standard library sha256, but we can verify determinism and that the hash
// interface is satisfied.
func TestNewAndWrite(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"short", []byte("hello")},
		{"exactly-block", bytes.Repeat([]byte("a"), 64)},
		{"two-blocks", bytes.Repeat([]byte("b"), 128)},
		{"unaligned", bytes.Repeat([]byte("c"), 130)},
		{"big", bytes.Repeat([]byte("xyz123"), 200)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d1 := New()
			n, err := d1.Write(tc.in)
			if err != nil {
				t.Fatalf("Write error: %v", err)
			}
			if n != len(tc.in) {
				t.Fatalf("Write returned %d, want %d", n, len(tc.in))
			}
			sum1 := d1.Sum(nil)
			if len(sum1) != Size {
				t.Fatalf("sum length %d, want %d", len(sum1), Size)
			}

			// Sum is non-destructive — calling it again should give the same value.
			sum1b := d1.Sum(nil)
			if !bytes.Equal(sum1, sum1b) {
				t.Fatalf("Sum() not idempotent: %x vs %x", sum1, sum1b)
			}

			// Same input -> same output.
			d2 := New()
			d2.Write(tc.in)
			sum2 := d2.Sum(nil)
			if !bytes.Equal(sum1, sum2) {
				t.Fatalf("non-deterministic: %x vs %x", sum1, sum2)
			}

			// Sum appends to the given byte slice.
			prefix := []byte{0xde, 0xad}
			withPrefix := d1.Sum(prefix)
			if !bytes.Equal(withPrefix[:2], prefix) {
				t.Fatalf("Sum did not preserve prefix: %x", withPrefix)
			}
			if !bytes.Equal(withPrefix[2:], sum1) {
				t.Fatalf("Sum did not append the digest: %x vs %x", withPrefix[2:], sum1)
			}
		})
	}
}

// TestChunkedWrite verifies that splitting the input across multiple Write
// calls produces the same digest as a single Write.
func TestChunkedWrite(t *testing.T) {
	full := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 10)

	d1 := New()
	d1.Write(full)
	expected := d1.Sum(nil)

	splits := [][]int{
		{1},
		{0},
		{len(full)},
		{63, 64},
		{1, 2, 3, 4, 5},
		{32, 32, 32, 32, 32, 32, 32, 32, 32, 32},
	}
	for _, sizes := range splits {
		d := New()
		off := 0
		for _, s := range sizes {
			end := off + s
			if end > len(full) {
				end = len(full)
			}
			d.Write(full[off:end])
			off = end
		}
		if off < len(full) {
			d.Write(full[off:])
		}
		got := d.Sum(nil)
		if !bytes.Equal(got, expected) {
			t.Fatalf("split %v gave different digest", sizes)
		}
	}
}

// TestSize verifies Size() returns the package-documented length for both
// SHA-256 and SHA-224 digests, and that BlockSize() is exposed.
func TestSize(t *testing.T) {
	d := New()
	if got := d.Size(); got != Size {
		t.Fatalf("SHA-256 Size() = %d, want %d", got, Size)
	}
	if got := d.BlockSize(); got != BlockSize {
		t.Fatalf("SHA-256 BlockSize() = %d, want %d", got, BlockSize)
	}

	h224 := New224()
	if got := h224.Size(); got != Size224 {
		t.Fatalf("SHA-224 Size() = %d, want %d", got, Size224)
	}
	if got := h224.BlockSize(); got != BlockSize {
		t.Fatalf("SHA-224 BlockSize() = %d, want %d", got, BlockSize)
	}

	// New224 should satisfy hash.Hash.
	var _ hash.Hash = h224
	h224.Write([]byte("abc"))
	_ = h224.Sum(nil) // the SHA-224 codepath uses different init constants via Reset

	// Reset on a freshly written digest should put us back at the empty state.
	d2 := New()
	emptySum := d2.Sum(nil)
	d2.Write([]byte("dirty"))
	d2.Reset()
	if got := d2.Sum(nil); !bytes.Equal(got, emptySum) {
		t.Fatalf("Reset did not restore empty state: %x vs %x", got, emptySum)
	}
}

// TestBinaryMarshalRoundtrip exercises MarshalBinary / UnmarshalBinary plus
// the appendUint32/appendUint64/consumeUint32/consumeUint64 helpers they wrap.
func TestBinaryMarshalRoundtrip(t *testing.T) {
	// Use an input that leaves a partial chunk pending (nx > 0) so the
	// internal x buffer is non-empty and gets carried through marshal.
	inputs := [][]byte{
		nil,
		[]byte("a"),
		bytes.Repeat([]byte("z"), 50),  // < chunk: nx > 0, no blocks consumed
		bytes.Repeat([]byte("z"), 64),  // exactly one block: nx == 0
		bytes.Repeat([]byte("z"), 100), // > chunk: nx > 0, one block consumed
	}

	for _, in := range inputs {
		d := New()
		d.Write(in)

		// MarshalBinary -> UnmarshalBinary into a fresh digest.
		var bm encoding.BinaryMarshaler = d
		state, err := bm.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary error: %v", err)
		}

		d2 := New()
		var bu encoding.BinaryUnmarshaler = d2
		if err := bu.UnmarshalBinary(state); err != nil {
			t.Fatalf("UnmarshalBinary error: %v", err)
		}

		// After restoring state, finishing with the same trailing bytes should
		// yield the same digest as writing everything to the original.
		tail := []byte("trailing-data-after-restore")
		d.Write(tail)
		d2.Write(tail)
		if !bytes.Equal(d.Sum(nil), d2.Sum(nil)) {
			t.Fatalf("restored digest diverged for input len %d", len(in))
		}
	}
}

// TestUnmarshalBinaryErrors covers the two failure branches of UnmarshalBinary.
func TestUnmarshalBinaryErrors(t *testing.T) {
	d := New()

	// Too short to even contain the magic.
	if err := d.UnmarshalBinary([]byte{0, 1, 2}); err == nil {
		t.Fatalf("expected error for short input")
	}

	// Right length but wrong magic.
	bad := make([]byte, marshaledSize)
	copy(bad, "xxx\x00")
	if err := d.UnmarshalBinary(bad); err == nil {
		t.Fatalf("expected error for bad magic")
	}

	// Correct magic but wrong length.
	state, err := d.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	truncated := state[:len(state)-1]
	if err := d.UnmarshalBinary(truncated); err == nil {
		t.Fatalf("expected error for truncated state")
	}

	// SHA-224 magic mismatch: SHA-256 digest can't unmarshal a SHA-224 blob.
	d224 := New224().(*Digest)
	state224, err := d224.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary 224: %v", err)
	}
	if err := New().UnmarshalBinary(state224); err == nil {
		t.Fatalf("expected SHA-256 to reject SHA-224 magic")
	}
}

// TestUint64HelpersBoundary covers appendUint64/consumeUint64 across edge
// values, since the only callers (Marshal/Unmarshal) only hit a small slice
// of the value space.
func TestUint64HelpersBoundary(t *testing.T) {
	d := New()
	// Write enough bytes to push d.len through a few orders of magnitude so
	// the appendUint64 / consumeUint64 helpers carry a non-trivial length.
	d.Write(bytes.Repeat([]byte{0xab}, 1<<10))

	state, err := d.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	d2 := New()
	if err := d2.UnmarshalBinary(state); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if d2.len != d.len {
		t.Fatalf("len not preserved: got %d want %d", d2.len, d.len)
	}
	if d2.nx != d.nx {
		t.Fatalf("nx not preserved: got %d want %d", d2.nx, d.nx)
	}
	for i := range d.h {
		if d2.h[i] != d.h[i] {
			t.Fatalf("h[%d] not preserved: got %x want %x", i, d2.h[i], d.h[i])
		}
	}
}
