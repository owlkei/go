//go:build race

package loadshed

// raceEnabled skips the timing acceptance runs (L12, L13): the race detector slows them below
// their thresholds. `make check` runs them again without it.
const raceEnabled = true
