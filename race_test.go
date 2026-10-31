//go:build race
// +build race

package mendoza

func init() {
	// The race detector changes how the runtime allocates, so the allocation tests are skipped.
	raceEnabled = true
}
