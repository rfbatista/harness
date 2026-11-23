// Command webbuild bundles the web client with esbuild's Go API, so the
// toolchain stays Go-only:
//
//	web/src/main.js       → internal/adapter/in/web/static/app.js
//	web/src/app.css       → internal/adapter/in/web/static/app.css (design system + xterm.css)
//	web/src/document.css  → internal/adapter/in/web/static/document.css (what an HTML task document may link)
//	web/src/code-worker.js → internal/adapter/in/web/static/code-worker.js (the code view's worker, a classic script)
//	web/app (React)       → internal/adapter/in/web/static/islands.server.js, islands-<hash>.js and manifest.json,
//	                        built by gohydrate (run `npm ci` in web/app first; `make web` does)
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
	"github.com/rfbatista/gohydrate"
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
	// The code views' highlighter, tracked (not in web/vendor): see its README.
	"highlight.js": "./web/third_party/highlightjs",
	// The swap regions' htmx, tracked too: see its README.
	"htmx.org": "./web/third_party/htmx/htmx.esm.js",
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
	workerOpts := workerOptions(root, dev)
	if !watch {
		if err := report(api.Build(opts)); err != nil {
			return err
		}
		if err := report(api.Build(docOpts)); err != nil {
			return err
		}
		if err := report(api.Build(workerOpts)); err != nil {
			return err
		}
		return buildIslands(root, dev)
	}
	// The islands are built once; rerun webbuild after changing web/app.
	if err := buildIslands(root, dev); err != nil {
		return err
	}

	for _, o := range []api.BuildOptions{opts, docOpts, workerOpts} {
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
		// htmx keeps a direct eval for hx-on and js: values; it is never
		// reached, since web/src/swap-regions.js sets allowEval to false.
		LogOverride: map[string]api.LogLevel{"direct-eval": api.LogLevelSilent},
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

// workerOptions builds the code worker as its own script: a worker is a
// separate realm, so it bundles what it needs (highlight.js) and is served
// beside app.js, from the same origin.
func workerOptions(root string, dev bool) api.BuildOptions {
	o := options(root, dev)
	o.EntryPoints = []string{"web/src/code-worker.js"}
	o.EntryNames = "code-worker"
	o.Format = api.FormatIIFE
	return o
}

// islandOptions builds the React client through gohydrate: the top bar's server
// entry, which the server renders in goja, and the browser's composition root.
// The bundles land beside the Alpine ones, under their own names.
func islandOptions(root string, dev bool) gohydrate.IslandBuildOptions {
	app := filepath.Join(root, "web", "app")
	return gohydrate.IslandBuildOptions{
		Islands:     []gohydrate.Island{{Name: "top-bar", ServerEntry: filepath.Join(app, "src", "islands", "top-bar.server.tsx")}},
		ClientEntry: filepath.Join(app, "src", "main.client.tsx"),
		ResolveDir:  app,
		OutDir:      filepath.Join(root, outDir),
		Minify:      !dev,
	}
}

func buildIslands(root string, dev bool) error {
	if _, err := os.Stat(filepath.Join(root, "web", "app", "node_modules")); err != nil {
		return errors.New("web/app has no node_modules; run `npm ci` in web/app (make web does)")
	}
	manifest, err := gohydrate.BuildIslands(islandOptions(root, dev))
	if err != nil {
		return fmt.Errorf("islands: %w", err)
	}
	fmt.Println("wrote", outDir+"/"+manifest.Client.JS, "and the island server bundle")
	return pruneStaleIslands(filepath.Join(root, outDir), manifest)
}

// pruneStaleIslands removes the islands-<hash> files an earlier build left
// behind: every one stays embedded in the binary and served, so they would
// pile up with each rebuild. It keeps the client bundle and stylesheet the
// manifest names, and their source maps.
func pruneStaleIslands(dir string, m gohydrate.Manifest) error {
	keep := map[string]bool{}
	for _, name := range []string{m.Client.JS, m.Client.CSS} {
		if name != "" {
			keep[name] = true
			keep[name+".map"] = true
		}
	}
	stale, err := filepath.Glob(filepath.Join(dir, "islands-*"))
	if err != nil {
		return err
	}
	for _, path := range stale {
		if keep[filepath.Base(path)] {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing the stale island bundle: %w", err)
		}
		fmt.Println("removed", outDir+"/"+filepath.Base(path))
	}
	return nil
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
