package sha256

import (
	"strings"
	"testing"
)

func TestWriteStringMatchesWrite(t *testing.T) {
	// Cover strings shorter than, equal to and longer than a block, written after a partial block.
	for _, prefix := range []int{0, 1, 63} {
		for _, n := range []int{0, 1, 55, 63, 64, 65, 200} {
			s := strings.Repeat("x", n)

			a := New()
			a.Write(make([]byte, prefix))
			a.Write([]byte(s))

			b := New()
			b.Write(make([]byte, prefix))
			b.WriteString(s)

			if a.CheckSum() != b.CheckSum() {
				t.Errorf("prefix %d, length %d: WriteString gives a different hash than Write", prefix, n)
			}
		}
	}
}

func TestWriteStringDoesNotAllocate(t *testing.T) {
	s := strings.Repeat("x", 1000)
	d := New()
	if allocs := testing.AllocsPerRun(100, func() { d.WriteString(s) }); allocs != 0 {
		t.Errorf("%v allocations, want 0", allocs)
	}
}
