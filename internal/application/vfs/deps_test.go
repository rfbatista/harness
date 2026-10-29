package vfs_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "operators-mcp/"

// TestTheVirtualFileSystemNeverImportsTheVirtualShell fails when this
// package, or any package of the repository it imports, depends on the
// virtual shell domain (ADR 0050): the virtual shell depends on the virtual
// file system, never the reverse.
func TestTheVirtualFileSystemNeverImportsTheVirtualShell(t *testing.T) {
	const shellPkg = modulePath + "internal/application/vshell"
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var walk func(pkg string, via []string)
	walk = func(pkg string, via []string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		if pkg == shellPkg {
			t.Errorf("the virtual file system depends on the virtual shell: %s", strings.Join(append(via, pkg), " -> "))
			return
		}
		p, err := build.ImportDir(filepath.Join(root, strings.TrimPrefix(pkg, modulePath)), 0)
		if err != nil {
			t.Fatalf("read %s: %v", pkg, err)
		}
		for _, imp := range p.Imports {
			if strings.HasPrefix(imp, modulePath) {
				walk(imp, append(via, pkg))
			}
		}
	}
	walk(modulePath+"internal/application/vfs", nil)
	if len(seen) < 2 {
		t.Fatalf("the walk read only %d packages; it is not following imports", len(seen))
	}
}
