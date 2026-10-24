package app

import (
	"strings"
	"testing"
)

// Playwright refuses a unix socket path over 103 bytes
// (<temp dir>/browser/<14 characters>.sock); the longest session temp dir is
// a macOS system temp dir (50 bytes, /var/folders/xx/<28>/T) plus the prefix
// and a start's "-" and nine random digits.
func TestSessionTempDirKeepsPlaywrightsSocketPathWithinTheLimit(t *testing.T) {
	const limit = 103
	base := "/var/folders/jf/kllvml0d3gn15fv7kpyd9xsr0000gn/T"
	if len(base) != 48 {
		t.Fatalf("test base is %d bytes", len(base))
	}
	longBase := base + "ab" // 50 bytes: the longest the platform makes
	dir := sessionTempPrefix(longBase, "96fd6757-0532-436b-a6c9-3e368b5f170c") + "-" + strings.Repeat("9", 9)
	socket := dir + "/browser/" + strings.Repeat("x", 14) + ".sock"
	if len(socket) > limit {
		t.Errorf("socket path %q is %d bytes, over %d", socket, len(socket), limit)
	}
}
