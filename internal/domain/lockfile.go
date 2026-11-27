package domain

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// Lockfiles (Glossary "Lockfile rule"): the pairs of lockfile and manifest the
// rule knows, and the dependencies a lockfile diff adds (ADR 0012).

// lockfileManifests maps a lockfile's base name to its manifest's, which sits in
// the same folder.
var lockfileManifests = map[string]string{
	"go.sum":              "go.mod",
	"go.work.sum":         "go.work",
	"package-lock.json":   "package.json",
	"npm-shrinkwrap.json": "package.json",
	"yarn.lock":           "package.json",
	"pnpm-lock.yaml":      "package.json",
	"bun.lock":            "package.json",
	"Cargo.lock":          "Cargo.toml",
	"poetry.lock":         "pyproject.toml",
	"uv.lock":             "pyproject.toml",
	"Pipfile.lock":        "Pipfile",
	"Gemfile.lock":        "Gemfile",
	"composer.lock":       "composer.json",
}

// LockfileManifest answers the manifest, as a path relative to the repository
// root, that the lockfile at rel is locked from; ok is false when rel is no
// lockfile the rule knows.
func LockfileManifest(rel string) (string, bool) {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	manifest, ok := lockfileManifests[path.Base(rel)]
	if !ok {
		return "", false
	}
	return path.Join(path.Dir(rel), manifest), true
}

var (
	goSumLine    = regexp.MustCompile(`^(\S+) (v\S+?)(/go\.mod)? h1:`)
	tomlName     = regexp.MustCompile(`^name = "([^"]+)"`)
	npmPackage   = regexp.MustCompile(`^"(?:node_modules/)+([^"]+)": \{`)
	yarnEntry    = regexp.MustCompile(`^"?(@?[^@\s"]+)@`)
	pnpmEntry    = regexp.MustCompile(`^'?/?(@?[^@\s':]+)@`)
	gemSpec      = regexp.MustCompile(`^    (\S+) \(`)
	composerName = regexp.MustCompile(`^"name": "([^"]+)"`)
)

// AddedDependencies lists the dependencies a diff of the lockfile at rel adds:
// the entries on its added lines, as the lockfile names them, without
// repeats. A removed or untouched entry is not listed; a lockfile the rule does
// not know lists nothing.
func AddedDependencies(rel, diff string) []string {
	base := path.Base(rel)
	if _, ok := lockfileManifests[base]; !ok {
		return nil
	}
	var out []string
	add := func(s string) {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		text := strings.TrimRight(line[1:], "\r")
		trimmed := strings.TrimSpace(text)
		switch base {
		case "go.sum", "go.work.sum":
			if m := goSumLine.FindStringSubmatch(trimmed); m != nil && m[3] == "" {
				add(m[1] + "@" + m[2])
			}
		case "Cargo.lock", "poetry.lock", "uv.lock":
			if m := tomlName.FindStringSubmatch(trimmed); m != nil {
				add(m[1])
			}
		case "package-lock.json", "npm-shrinkwrap.json":
			if m := npmPackage.FindStringSubmatch(trimmed); m != nil {
				add(m[1])
			}
		case "yarn.lock":
			if text == trimmed {
				if m := yarnEntry.FindStringSubmatch(trimmed); m != nil {
					add(m[1])
				}
			}
		case "pnpm-lock.yaml":
			if m := pnpmEntry.FindStringSubmatch(trimmed); m != nil && strings.HasSuffix(trimmed, ":") {
				add(m[1])
			}
		case "Gemfile.lock":
			if m := gemSpec.FindStringSubmatch(text); m != nil {
				add(m[1])
			}
		case "composer.lock", "Pipfile.lock":
			if m := composerName.FindStringSubmatch(trimmed); m != nil {
				add(m[1])
			}
		}
	}
	return out
}
