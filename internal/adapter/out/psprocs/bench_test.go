package psprocs

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkParse800Processes is the parsing half of the vigia's one table read
// per second: the busiest machine is in the hundreds of processes.
func BenchmarkParse800Processes(b *testing.B) {
	var sb strings.Builder
	for i := 1; i <= 800; i++ {
		fmt.Fprintf(&sb, "%5d %5d 0xabc%d %7d Thu Nov 26 08:59:31 2026 /usr/local/bin/node /repo/node_modules/.bin/jest --runInBand --session-id c-%d\n", i, i/2, i%20, 100000+i, i)
	}
	in := sb.String()
	b.ResetTimer()
	for range b.N {
		Parse(in)
	}
}
