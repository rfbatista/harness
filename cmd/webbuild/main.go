// Command webbuild bundles the web client with esbuild's Go API, so the
// toolchain stays Go-only:
//
//	web/src/main.js       → internal/adapter/in/web/static/app.js
//	web/src/app.css       → internal/adapter/in/web/static/app.css (design system + xterm.css)
//	web/src/document.css  → internal/adapter/in/web/static/document.css (what an HTML task document may link)
//
// Bare imports resolve to the vendored files in web/vendor (vendors). It also writes
// web/test/all.js, the list of every *.test.js the browser test page runs.
//
// Usage, from the repository root:
//
//	go run ./cmd/webbuild           build once
//	go run ./cmd/webbuild -watch    rebuild on change until interrupted
//	go run ./cmd/webbuild -dev      unminified output (implied by -watch)
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/evanw/esbuild/pkg/api"
)

const (
	outDir      = "internal/adapter/in/web/static"
	testsDir    = "web/src"
	testsList   = "web/test/all.js"
	alpineAlias = "./web/vendor/alpine-csp.esm.js"
)

// vendors maps the bare imports main.js uses to web/vendor.
var vendors = map[string]string{
	"alpinejs":         alpineAlias,
	"@xterm/xterm":     "./web/vendor/xterm.esm.js",
	"@xterm/addon-fit": "./web/vendor/xterm-addon-fit.esm.js",
}

// engines are the browsers the web client supports; esbuild lowers syntax
// (CSS nesting included) only below these.
var engines = []api.Engine{
	{Name: api.EngineChrome, Version: "120"},
	{Name: api.EngineFirefox, Version: "121"},
	{Name: api.EngineSafari, Version: "17.2"},
}

func main() {
	watch := flag.Bool("watch", false, "rebuild on change until interrupted")
	dev := flag.Bool("dev", false, "skip minification")
	flag.Parse()

	if err := run(*watch, *dev || *watch); err != nil {
		fmt.Fprintln(os.Stderr, "webbuild:", err)
		os.Exit(1)
	}
}

func run(watch, dev bool) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "web", "src", "main.js")); err != nil {
		return errors.New("run from the repository root (web/src/main.js not found)")
	}
	if err := WriteTestList(root); err != nil {
		return err
	}

	opts := options(root, dev)
	docOpts := documentOptions(root, dev)
	if !watch {
		if err := report(api.Build(opts)); err != nil {
			return err
		}
		return report(api.Build(docOpts))
	}

	for _, o := range []api.BuildOptions{opts, docOpts} {
		o.Plugins = append(o.Plugins, rebuildLogger())
		ctx, ctxErr := api.Context(o)
		if ctxErr != nil {
			return ctxErr
		}
		defer ctx.Dispose()
		if err := ctx.Watch(api.WatchOptions{}); err != nil {
			return err
		}
	}
	fmt.Println("webbuild: watching web/src, web/vendor and design-system/css (ctrl-c to stop)")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

func options(root string, dev bool) api.BuildOptions {
	return api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints: []string{
			"web/src/main.js",
			"web/src/app.css",
		},
		EntryNames:        "app",
		Outdir:            outDir,
		Bundle:            true,
		Write:             true,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		Engines:           engines,
		Alias:             vendors,
		Sourcemap:         api.SourceMapLinked,
		MinifyWhitespace:  !dev,
		MinifySyntax:      !dev,
		MinifyIdentifiers: !dev,
		LegalComments:     api.LegalCommentsEndOfFile,
		LogLevel:          api.LogLevelWarning,
	}
}

// documentOptions builds the document stylesheet on its own, under its own
// entry name: the app build names every output "app".
func documentOptions(root string, dev bool) api.BuildOptions {
	o := options(root, dev)
	o.EntryPoints = []string{"web/src/document.css"}
	o.EntryNames = "document"
	return o
}

// report fails the build on any error or warning: a warning in a bundle we
// ship is a bug we have not looked at yet.
func report(result api.BuildResult) error {
	if len(result.Errors) > 0 || len(result.Warnings) > 0 {
		return fmt.Errorf("%d error(s), %d warning(s)", len(result.Errors), len(result.Warnings))
	}
	for _, f := range result.OutputFiles {
		fmt.Println("wrote", f.Path)
	}
	if len(result.OutputFiles) == 0 {
		fmt.Println("wrote", outDir+"/*")
	}
	return nil
}

func rebuildLogger() api.Plugin {
	return api.Plugin{
		Name: "rebuild-log",
		Setup: func(b api.PluginBuild) {
			b.OnEnd(func(r *api.BuildResult) (api.OnEndResult, error) {
				if len(r.Errors) == 0 {
					fmt.Println("webbuild: rebuilt")
				}
				return api.OnEndResult{}, nil
			})
		},
	}
}

// WriteTestList writes web/test/all.js: one import per *.test.js under
// web/src, in path order. It leaves the file alone when it is already current.
func WriteTestList(root string) error {
	content, err := TestList(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, testsList)
	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestList renders web/test/all.js for the test files under web/src.
func TestList(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(root, testsDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".test.js") {
			rel, err := filepath.Rel(filepath.Join(root, "web"), path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(files)

	var b strings.Builder
	b.WriteString("// Generated by `go run ./cmd/webbuild` from web/src/**/*.test.js. Do not edit.\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "import \"../%s\";\n", f)
	}
	return b.String(), nil
}
