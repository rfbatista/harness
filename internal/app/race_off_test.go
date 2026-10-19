//go:build !race

package app

// raceEnabled is whether the race detector is on: it slows everything
// several times over, so timing budgets are not held under it.
const raceEnabled = false
