//go:build !windows

package shim

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// parse is what the command name with args would do to files, as edits
// for the shell endpoints, with absolute paths. Nil: it writes no file
// (sed without -i, or arguments it cannot use).
func parse(name string, args []string, real, cwd string) []edit {
	p := paths{cwd}
	switch name {
	case "sed":
		return parseSed(args, real, p)
	case "tee":
		_, files := scan(args, spec{})
		return p.one("write", files)
	case "touch":
		_, files := scan(args, spec{shortArg: "drtA", longArg: []string{"date", "reference", "time"}})
		return p.one("write", files)
	case "rm":
		_, files := scan(args, spec{})
		return p.one("rm", files)
	case "mkdir":
		_, dirs := scan(args, spec{shortArg: "m", longArg: []string{"mode"}})
		return p.one("mkdir", dirs)
	case "cp", "mv":
		return parseCopy(name, args, p)
	}
	return nil
}

// spec says which options take an argument: shortArg letters (attached or
// the next argument), shortOpt letters (attached only, possibly empty) and
// longArg names (after = or the next argument).
type spec struct {
	shortArg, shortOpt string
	longArg            []string
}

type opt struct {
	name, val string
}

// scan splits args into options and operands, getopt_long style: options
// may come anywhere before "--".
func scan(args []string, sp spec) (opts []opt, operands []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return opts, append(operands, args[i+1:]...)
		case strings.HasPrefix(a, "--"):
			name, val, has := strings.Cut(a[2:], "=")
			if !has && slices.Contains(sp.longArg, name) && i+1 < len(args) {
				i++
				val = args[i]
			}
			opts = append(opts, opt{name, val})
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c, rest := a[j:j+1], a[j+1:]
				if strings.Contains(sp.shortOpt, c) {
					opts = append(opts, opt{c, rest})
					break
				}
				if strings.Contains(sp.shortArg, c) {
					if rest == "" && i+1 < len(args) {
						i++
						rest = args[i]
					}
					opts = append(opts, opt{c, rest})
					break
				}
				opts = append(opts, opt{name: c})
			}
		default:
			operands = append(operands, a)
		}
	}
	return opts, operands
}

func has(opts []opt, names ...string) (opt, bool) {
	for _, o := range opts {
		if slices.Contains(names, o.name) {
			return o, true
		}
	}
	return opt{}, false
}

// parseSed: sed -i edits its files in place, and writes a backup of each
// when given a suffix. GNU sed takes the suffix attached to -i only; BSD
// sed takes the next argument when none is attached.
func parseSed(args []string, real string, p paths) []edit {
	if !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "-") && strings.ContainsAny(a, "iI") }) {
		return nil // no -i: not worth asking which sed this is
	}
	sp := spec{shortArg: "efl", longArg: []string{"expression", "file", "line-length"}}
	if gnuSed(real) {
		sp.shortOpt = "i"
	} else {
		sp.shortArg += "iI"
	}
	opts, operands := scan(args, sp)
	in, ok := has(opts, "i", "I", "in-place")
	if !ok {
		return nil
	}
	files := operands
	if _, script := has(opts, "e", "f", "expression", "file"); !script && len(files) > 0 {
		files = files[1:]
	}
	var out []string
	for _, f := range files {
		abs := p.abs(f)
		out = append(out, abs)
		if in.val != "" {
			out = append(out, backup(abs, in.val))
		}
	}
	return p.edit("write", out)
}

// backup is where sed -i SUFFIX keeps file's old copy: a * in the suffix
// stands for the file's name.
func backup(file, suffix string) string {
	if !strings.Contains(suffix, "*") {
		return file + suffix
	}
	dir, base := filepath.Split(file)
	b := strings.ReplaceAll(suffix, "*", base)
	if filepath.IsAbs(b) {
		return filepath.Clean(b)
	}
	return filepath.Join(dir, b)
}

func gnuSed(real string) bool {
	out, err := exec.Command(real, "--version").Output()
	return err == nil && strings.Contains(string(out), "GNU")
}

// parseCopy: cp and mv put each source at its final path: inside the
// target when it is a directory (or -t names one, or there are several
// sources), else at the target. One edit per source.
func parseCopy(name string, args []string, p paths) []edit {
	opts, operands := scan(args, spec{shortArg: "tS", longArg: []string{"target-directory", "suffix"}})
	var target string
	if t, ok := has(opts, "t", "target-directory"); ok {
		target = t.val
	} else if len(operands) >= 2 {
		target, operands = operands[len(operands)-1], operands[:len(operands)-1]
	} else {
		return nil
	}
	_, noTarget := has(opts, "T", "no-target-directory")
	_, toDir := has(opts, "t", "target-directory")
	if !noTarget && (toDir || len(operands) > 1 || isDir(p.abs(target))) {
		toDir = true
	}
	_, recursive := has(opts, "R", "r", "a", "recursive", "archive")
	var out []edit
	for _, src := range operands {
		dst := target
		if toDir {
			dst = filepath.Join(target, filepath.Base(filepath.Clean(src)))
		}
		e := edit{op: name, paths: []string{p.abs(src), p.abs(dst)}}
		e.walk = name == "cp" && recursive && isDir(e.paths[0])
		out = append(out, e)
	}
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// paths makes operands absolute against the working directory, with the
// symlinks of their parent directories resolved, so a path through a link
// into the shell is seen as inside it.
type paths struct{ cwd string }

func (p paths) abs(name string) string {
	if !filepath.IsAbs(name) {
		name = filepath.Join(p.cwd, name)
	}
	name = filepath.Clean(name)
	dir, rest := filepath.Dir(name), filepath.Base(name)
	for {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return name
		}
		dir, rest = parent, filepath.Join(filepath.Base(dir), rest)
	}
}

// one is a single edit of every operand.
func (p paths) one(op string, operands []string) []edit {
	var out []string
	for _, o := range operands {
		out = append(out, p.abs(o))
	}
	return p.edit(op, out)
}

func (paths) edit(op string, abs []string) []edit {
	if len(abs) == 0 {
		return nil
	}
	return []edit{{op: op, paths: abs}}
}
