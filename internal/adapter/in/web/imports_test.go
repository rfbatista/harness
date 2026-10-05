package web

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The browser code's layering (docs/WEB.md, "Dependency rules"), checked on
// every import statement under web/src.

const webSrc = "../../../../web/src"

// bareAllowed are the vendored libraries and the one file each may be
// imported from: the composition root, and the screen adapter.
var bareAllowed = map[string]bool{
	"main.js alpinejs":                 true,
	"xterm-screen.js @xterm/xterm":     true,
	"xterm-screen.js @xterm/addon-fit": true,
}

var importRe = regexp.MustCompile(`(?m)^\s*import\s[^"']*["']([^"']+)["']`)

type jsFile struct{ path, module, layer string }

func classify(p string) jsFile {
	f := jsFile{path: p, module: "shared", layer: "root"}
	parts := strings.Split(p, "/")
	if len(parts) > 1 && parts[0] == "modules" {
		f.module = parts[1]
	}
	for _, l := range []string{"domain", "infrastructure", "presentation", "testing"} {
		if strings.Contains("/"+p, "/"+l+"/") {
			f.layer = l
			break
		}
	}
	return f
}

func TestBrowserCodeRespectsTheLayers(t *testing.T) {
	var violations []string
	err := filepath.WalkDir(webSrc, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		rel, _ := filepath.Rel(webSrc, p)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		from := classify(rel)
		isTest := strings.HasSuffix(rel, ".test.js")

		for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			fail := func(why string) { violations = append(violations, rel+" imports "+spec+": "+why) }

			if !strings.HasPrefix(spec, ".") {
				if !bareAllowed[rel+" "+spec] {
					fail("bare imports (vendored libraries) belong in main.js and xterm-screen.js only")
				}
				continue
			}
			if !strings.HasSuffix(spec, ".js") && !strings.HasSuffix(spec, ".json") {
				fail("relative imports spell out the extension")
			}
			target := path.Clean(path.Join(path.Dir(rel), spec))
			if strings.HasPrefix(target, "../") {
				if !isTest {
					fail("only tests reach outside web/src")
				}
				continue
			}
			to := classify(target)

			if to.layer == "testing" && !isTest && from.layer != "testing" {
				fail("only tests import testing/")
			}
			if isTest || rel == "main.js" {
				continue
			}
			if from.module != to.module && to.module != "shared" {
				fail("modules never import each other")
			}
			if from.module == "shared" && to.module != "shared" {
				fail("shared never imports a module")
			}
			if strings.HasSuffix(target, "/register.js") && from.layer != "testing" {
				fail("only main.js imports register.js")
			}
			switch from.layer {
			case "domain":
				if to.layer != "domain" {
					fail("domain imports only domain")
				}
			case "infrastructure":
				if to.layer == "presentation" {
					fail("infrastructure never imports presentation")
				}
			case "presentation":
				if to.layer == "infrastructure" {
					fail("presentation reaches data through ports, never infrastructure")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]jsFile{
		"main.js":                      {"main.js", "shared", "root"},
		"shared/infrastructure/api.js": {"shared/infrastructure/api.js", "shared", "infrastructure"},
		"modules/sessions/presentation/pages/x.js": {"modules/sessions/presentation/pages/x.js", "sessions", "presentation"},
		"modules/sessions/testing/fixtures.js":     {"modules/sessions/testing/fixtures.js", "sessions", "testing"},
	}
	for in, want := range cases {
		if got := classify(in); got != want {
			t.Errorf("classify(%q) = %+v, want %+v", in, got, want)
		}
	}
}
