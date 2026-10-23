package keychainshim

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnMacOSTheDirLinksSecurityToTheShimBinaryAndGoesOnClose(t *testing.T) {
	d, err := New("/opt/harness/server", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(d.Path(), "security"))
	if err != nil || target != "/opt/harness/server" {
		t.Fatalf("security links to %q (%v)", target, err)
	}
	d.Close()
	if _, err := os.Stat(d.Path()); !os.IsNotExist(err) {
		t.Fatalf("dir survives Close: %v", err)
	}
}

func TestOffMacOSThereIsNoShimDir(t *testing.T) {
	d, err := New("/opt/harness/server", "linux")
	if err != nil || d.Path() != "" {
		t.Fatalf("linux dir = %q, %v", d.Path(), err)
	}
	if d, _ = New("", "darwin"); d.Path() != "" {
		t.Fatalf("no shim binary: dir = %q", d.Path())
	}
	d.Close()
}
