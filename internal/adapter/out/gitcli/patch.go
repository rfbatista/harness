package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// diffFlags fix the diff's shape whatever the user's diff config says: no
// colour, no external diff or textconv driver, a/ and b/ prefixes, three
// lines of context, renames and copies found.
var diffFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/", "-U3", "-M", "-C"}

// diffSides are the revision arguments of base → head; head "" is the
// worktree on disk.
//
// Diffing against the worktree runs the clean filters the repository itself
// configures (git-lfs, for one), as git status does on the history page:
// without them every filtered file would look modified. They are the
// repository's own configuration; nothing here fetches, writes or runs hooks.
func diffSides(base, head string) []string {
	if head == "" {
		return []string{"--end-of-options", base}
	}
	return []string{"--end-of-options", base, head}
}

// Changes lists the files changed from base to head with their line counts,
// in one git diff: its raw records (":<modes> <oids> <status>\0path\0",
// a second path for a rename or copy), then its numstat ones.
func (c Code) Changes(ctx context.Context, dir, base, head string) ([]ports.GitChange, error) {
	args := append([]string{"diff", "--raw", "--numstat", "-z", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "-C"}, diffSides(base, head)...)
	out, err := c.d.codeOutput(ctx, dir, append(args, "--")...)
	if err != nil {
		return nil, err
	}
	var changes []ports.GitChange
	recs := strings.Split(out, "\x00")
	i := 0
	for ; i < len(recs) && strings.HasPrefix(recs[i], ":"); i++ {
		f := strings.Fields(recs[i])
		if len(f) < 5 || i+1 >= len(recs) {
			return nil, fmt.Errorf("git diff --raw: unexpected record %q", recs[i])
		}
		i++
		ch := ports.GitChange{Path: recs[i], Added: -1, Deleted: -1}
		switch f[4][0] {
		case 'A':
			ch.Status = domain.ChangeAdded
		case 'D':
			ch.Status = domain.ChangeDeleted
		case 'R', 'C':
			if i+1 >= len(recs) {
				return nil, fmt.Errorf("git diff --raw: rename without a destination")
			}
			i++
			ch.OldPath, ch.Path = ch.Path, recs[i]
			ch.Status = domain.ChangeRenamed
			if f[4][0] == 'C' {
				ch.Status = domain.ChangeCopied
			}
		case 'T':
			ch.Status = domain.ChangeTypeChanged
		default: // M, U (unmerged), X
			ch.Status = domain.ChangeModified
		}
		changes = append(changes, ch)
	}
	counts := parseNumstatZ(strings.Join(recs[i:], "\x00"))
	for j := range changes {
		if n, ok := counts[changes[j].Path]; ok {
			changes[j].Added, changes[j].Deleted = n[0], n[1]
		}
	}
	return changes, nil
}

// patchBatch bounds the paths of one git diff command line.
const patchBatch = 500

// Patch reads the hunks of paths from base to head, keyed by each file's
// (new) path. A rename's old path must be among paths for git to pair it.
func (c Code) Patch(ctx context.Context, dir, base, head string, paths []string, maxLines int, maxBytes int64) (map[string]*domain.FileDiff, error) {
	out := map[string]*domain.FileDiff{}
	for start := 0; start < len(paths); start += patchBatch {
		batch := paths[start:min(start+patchBatch, len(paths))]
		args := append(append(append([]string{"diff", "-p"}, diffFlags...), diffSides(base, head)...), "--")
		if err := c.d.streamPatch(ctx, dir, append(args, batch...), maxLines, maxBytes, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// errPatchDone stops reading once the only file asked for is past a limit.
var errPatchDone = errors.New("patch read far enough")

func (d config) streamPatch(ctx context.Context, dir string, args []string, maxLines int, maxBytes int64, out map[string]*domain.FileDiff) error {
	ctx, cancel := context.WithTimeout(ctx, d.read())
	defer cancel()
	cmd := d.codeCmd(ctx, dir, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	single := len(args) > 0 && countPaths(args) == 1
	parseErr := parsePatch(bufio.NewReaderSize(stdout, 64<<10), maxLines, maxBytes, single, out)
	if parseErr != nil {
		cancel()
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	if err := cut(ctx, "diff", waitErr); timedOut(err) && parseErr != errPatchDone {
		return err
	}
	switch {
	case parseErr == errPatchDone:
		return nil
	case parseErr != nil:
		return parseErr
	case waitErr != nil:
		return fmt.Errorf("git diff: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// countPaths counts the paths after "--".
func countPaths(args []string) int {
	for i, a := range args {
		if a == "--" {
			return len(args) - i - 1
		}
	}
	return 0
}

// patchFile is the file being read and what it has cost so far.
type patchFile struct {
	fd       *domain.FileDiff
	lines    int
	bytes    int64
	over     bool
	old, new int
}

// parsePatch reads a unified diff into out. A file past maxLines changed
// lines or maxBytes of hunk text is kept with no hunks, withheld as
// too_large. With single, reading stops there.
func parsePatch(r *bufio.Reader, maxLines int, maxBytes int64, single bool, out map[string]*domain.FileDiff) error {
	var cur *patchFile
	var oldName string
	for {
		line, tooLong, err := readLine(r, maxBytes+headerRoom)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldName = ""
			cur = &patchFile{fd: &domain.FileDiff{}}
			cur.fd.Path = gitHeaderPath(strings.TrimPrefix(line, "diff --git "))
			out[cur.fd.Path] = cur.fd
		case cur == nil:
		case strings.HasPrefix(line, "@@ "):
			h, err := parseHunkHeader(line)
			if err != nil {
				return err
			}
			cur.old, cur.new = h.OldStart, h.NewStart
			if !cur.over {
				cur.fd.Hunks = append(cur.fd.Hunks, h)
			}
		case len(cur.fd.Hunks) == 0 && !cur.over:
			// Extended headers before the first hunk.
			switch {
			case strings.HasPrefix(line, "--- "):
				oldName = stripPrefix(unquote(trimName(line[4:])), "a/")
			case strings.HasPrefix(line, "+++ "):
				if name := trimName(line[4:]); name != "/dev/null" {
					rekey(out, cur, stripPrefix(unquote(name), "b/"))
				} else if oldName != "" {
					rekey(out, cur, oldName)
				}
			case strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy to "):
				_, name, _ := strings.Cut(line, " to ")
				rekey(out, cur, unquote(name))
			case strings.HasPrefix(line, "Binary files "):
				cur.fd.Binary = true
				why := domain.WithheldBinary
				cur.fd.Withheld = &why
			}
		default:
			if cur.over {
				continue
			}
			hunk := &cur.fd.Hunks[len(cur.fd.Hunks)-1]
			if line == "" {
				continue
			}
			if line[0] == '\\' {
				if n := len(hunk.Lines); n > 0 {
					hunk.Lines[n-1].NoNewline = true
				}
				continue
			}
			hl := domain.HunkLine{Text: line[1:]}
			switch line[0] {
			case '+':
				hl.Kind, hl.New = domain.LineAdd, intp(cur.new)
				cur.new++
				cur.lines++
			case '-':
				hl.Kind, hl.Old = domain.LineDel, intp(cur.old)
				cur.old++
				cur.lines++
			default:
				hl.Kind, hl.Old, hl.New = domain.LineContext, intp(cur.old), intp(cur.new)
				cur.old++
				cur.new++
			}
			cur.bytes += int64(len(line))
			if tooLong || cur.lines > maxLines || cur.bytes > maxBytes {
				cur.over = true
				cur.fd.Hunks = nil
				why := domain.WithheldTooLarge
				cur.fd.Withheld = &why
				if single {
					return errPatchDone
				}
				continue
			}
			hunk.Lines = append(hunk.Lines, hl)
		}
	}
}

// rekey files the diff under its real name, once the headers tell it.
func rekey(out map[string]*domain.FileDiff, cur *patchFile, name string) {
	if name == cur.fd.Path {
		return
	}
	if out[cur.fd.Path] == cur.fd {
		delete(out, cur.fd.Path)
	}
	cur.fd.Path = name
	out[name] = cur.fd
}

// headerRoom lets a header line through whatever the byte cap.
const headerRoom = 4096

// readLine reads one line without its newline. A line longer than max is
// read through but cut, and reported too long.
func readLine(r *bufio.Reader, max int64) (string, bool, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(buf)+len(chunk) <= int(max) {
			buf = append(buf, chunk...)
		} else {
			tooLong = true
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && (err != io.EOF || len(buf) == 0) {
			return "", false, err
		}
		return strings.TrimSuffix(string(buf), "\n"), tooLong, nil
	}
}

func intp(n int) *int { return &n }

// parseHunkHeader reads "@@ -a[,b] +c[,d] @@ header".
func parseHunkHeader(line string) (domain.Hunk, error) {
	rest := strings.TrimPrefix(line, "@@ ")
	ranges, header, ok := strings.Cut(rest, " @@")
	if !ok {
		return domain.Hunk{}, fmt.Errorf("git diff: bad hunk header %q", line)
	}
	oldR, newR, ok := strings.Cut(ranges, " ")
	if !ok {
		return domain.Hunk{}, fmt.Errorf("git diff: bad hunk header %q", line)
	}
	h := domain.Hunk{Header: strings.TrimPrefix(header, " "), Lines: []domain.HunkLine{}}
	var err1, err2 error
	h.OldStart, h.OldLines, err1 = parseRange(strings.TrimPrefix(oldR, "-"))
	h.NewStart, h.NewLines, err2 = parseRange(strings.TrimPrefix(newR, "+"))
	if err1 != nil || err2 != nil {
		return domain.Hunk{}, fmt.Errorf("git diff: bad hunk header %q", line)
	}
	return h, nil
}

func parseRange(s string) (int, int, error) {
	start, count, hasCount := strings.Cut(s, ",")
	a, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, err
	}
	if !hasCount {
		return a, 1, nil
	}
	b, err := strconv.Atoi(count)
	return a, b, err
}

// gitHeaderPath reads the path of "a/<p> b/<p>" when both sides name the
// same file (a mode change, or before ---/+++ name it); a quoted pair is
// unquoted. A rename's real names come from its own headers.
func gitHeaderPath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if end := closingQuote(s); end > 0 {
			return stripPrefix(unquote(s[:end+1]), "a/")
		}
	}
	if len(s) >= 5 && (len(s)-1)%2 == 0 {
		half := (len(s) - 1) / 2
		if s[:half][2:] == s[half+1:][2:] {
			return s[2:half]
		}
	}
	a, _, _ := strings.Cut(s, " b/")
	return strings.TrimPrefix(a, "a/")
}

func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// trimName drops the tab git appends to a ---/+++ name holding a space.
func trimName(s string) string { return strings.TrimSuffix(s, "\t") }

func stripPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

// unquote reads git's C-style quoting of a name ("a\tb", octal escapes for
// bytes); an unquoted name comes back as it is.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch e := s[i]; e {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3':
			if i+2 < len(s) {
				if n, err := strconv.ParseUint(s[i:i+3], 8, 8); err == nil {
					b.WriteByte(byte(n))
					i += 2
					continue
				}
			}
			b.WriteByte(e)
		default:
			b.WriteByte(e)
		}
	}
	return b.String()
}
