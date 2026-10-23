package claudehome

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ClaudeTranscriptReader = (*TranscriptReader)(nil)

// maxIndexes is how many transcripts keep their index in memory.
const maxIndexes = 32

// conversationID is what a conversation id may look like: the CLI's are
// UUIDs. Anything else could name a path, so it never reaches the disk.
var conversationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,127}$`)

// TranscriptReader reads conversation transcripts a page at a time, strictly
// read-only and only inside the CLI's projects directory.
//
// The CLI only appends to a transcript, so each file gets an index of where
// its entries are — a line's offset and length and the entry's place in it —
// extended with what was appended since the last request. A request that
// finds the file unchanged costs one stat; a page re-reads only its own
// lines.
type TranscriptReader struct {
	// Dir is the CLI's config directory; empty means $CLAUDE_CONFIG_DIR, else
	// ~/.claude.
	Dir string
	// Legacy is searched, read-only, for what Dir does not have (see
	// Transcripts.Legacy). Dir wins when both hold a conversation.
	Legacy string

	mu      sync.Mutex
	paths   map[string]string // conversation id → config dir + path relative to projects/
	indexes map[string]*index // by config dir + relative path
	clock   uint64
}

// NewTranscriptReader returns a reader of the transcripts under dir (see
// TranscriptReader.Dir).
func NewTranscriptReader(dir string) *TranscriptReader { return &TranscriptReader{Dir: dir} }

func missing(id string) error {
	return &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "claude has no saved conversation " + id}
}

func badCursor() error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: "unknown transcript cursor"}
}

// ReadTranscript implements ports.ClaudeTranscriptReader.
func (r *TranscriptReader) ReadTranscript(dir, id string, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	if !conversationID.MatchString(id) {
		return ports.TranscriptPage{}, missing(id)
	}
	var f *os.File
	var rel string
	for _, dir0 := range (Transcripts{Dir: r.Dir, Legacy: r.Legacy}).roots() {
		root, err := os.OpenRoot(filepath.Join(dir0, "projects"))
		if err != nil {
			continue
		}
		rel, f = r.open(root, dir0, dir, id)
		root.Close()
		if f != nil {
			break
		}
	}
	if f == nil {
		return ports.TranscriptPage{}, missing(id)
	}
	defer f.Close()

	idx := r.index(rel)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if err := idx.refresh(f); err != nil {
		return ports.TranscriptPage{}, missing(id)
	}
	return idx.page(f, q)
}

// open finds conversation id's transcript and opens it for reading. The CLI
// files it under the directory it ran in, spelled as given or with symlinks
// resolved; when that directory is gone and neither spelling finds it, the
// projects directory is searched for the id, which is a UUID.
func (r *TranscriptReader) open(root *os.Root, home, dir, id string) (string, *os.File) {
	name := id + ".jsonl"
	r.mu.Lock()
	cached, ok := strings.CutPrefix(r.paths[id], home+"\x00")
	r.mu.Unlock()
	if !ok {
		cached = ""
	}

	candidates := []string{cached}
	if dir != "" {
		candidates = append(candidates, nonAlnum.ReplaceAllString(dir, "-")+"/"+name)
		if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
			candidates = append(candidates, nonAlnum.ReplaceAllString(real, "-")+"/"+name)
		}
	}
	for _, rel := range candidates {
		if rel == "" {
			continue
		}
		if f, err := openFile(root, rel); err == nil {
			r.remember(id, home+"\x00"+rel)
			return home + "\x00" + rel, f
		}
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return "", nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := e.Name() + "/" + name
		if f, err := openFile(root, rel); err == nil {
			r.remember(id, home+"\x00"+rel)
			return home + "\x00" + rel, f
		}
	}
	return "", nil
}

// openFile opens a regular file under root, read-only. Root refuses any
// path or symlink that leads outside it.
func openFile(root *os.Root, rel string) (*os.File, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}

func (r *TranscriptReader) remember(id, rel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.paths == nil {
		r.paths = map[string]string{}
	}
	r.paths[id] = rel
}

// index returns rel's index, creating it and evicting the least recently
// used one past maxIndexes.
func (r *TranscriptReader) index(rel string) *index {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexes == nil {
		r.indexes = map[string]*index{}
	}
	r.clock++
	idx, ok := r.indexes[rel]
	if !ok {
		if len(r.indexes) >= maxIndexes {
			var oldest string
			for k, v := range r.indexes {
				if oldest == "" || v.used < r.indexes[oldest].used {
					oldest = k
				}
			}
			delete(r.indexes, oldest)
		}
		idx = &index{}
		r.indexes[rel] = idx
	}
	idx.used = r.clock
	return idx
}

// slot is where one entry is: its line and its place among the line's
// entries, plus the tool it names.
type slot struct {
	off  int64
	n    int32 // line length, without the newline
	part int32
	tool string
}

func (s slot) cursor() string {
	return strconv.FormatInt(s.off, 10) + "." + strconv.FormatInt(int64(s.part), 10)
}

func parseCursor(c string) (off int64, part int32, ok bool) {
	o, p, found := strings.Cut(c, ".")
	if !found {
		return 0, 0, false
	}
	off, err1 := strconv.ParseInt(o, 10, 64)
	pp, err2 := strconv.ParseInt(p, 10, 32)
	if err1 != nil || err2 != nil || off < 0 || pp < 0 {
		return 0, 0, false
	}
	// Only the spelling slot.cursor writes: one cursor per entry.
	if (slot{off: off, part: int32(pp)}).cursor() != c {
		return 0, 0, false
	}
	return off, int32(pp), true
}

// index is one transcript's entries, as far as the file has complete lines.
type index struct {
	mu    sync.Mutex
	used  uint64 // under TranscriptReader.mu
	file  os.FileInfo
	end   int64 // bytes indexed: always just after a newline
	slots []slot
	tools map[string]string // tool_use id → tool name
}

// refresh brings the index up to the file's complete lines. A file that
// shrank or was replaced is indexed again from the start.
func (x *index) refresh(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if x.file == nil || !os.SameFile(x.file, fi) || fi.Size() < x.end {
		x.end, x.slots, x.tools = 0, nil, nil
	}
	x.file = fi
	if fi.Size() == x.end {
		return nil
	}
	if x.tools == nil {
		x.tools = map[string]string{}
	}
	toolName := func(id string) string { return x.tools[id] }
	br := bufio.NewReaderSize(io.NewSectionReader(f, x.end, fi.Size()-x.end), 64<<10)
	off := x.end
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			// A line without its newline is still being written: it is
			// indexed once it is complete.
			break
		}
		body := line[:len(line)-1]
		for i, m := range mapLine(body, toolName) {
			x.slots = append(x.slots, slot{off: off, n: int32(len(body)), part: int32(i), tool: m.Tool})
			if m.toolUseID != "" {
				x.tools[m.toolUseID] = m.Tool
			}
		}
		off += int64(len(line))
	}
	x.end = off
	return nil
}

// find is the position of the entry under cursor c.
func (x *index) find(c string) (int, bool) {
	off, part, ok := parseCursor(c)
	if !ok {
		return 0, false
	}
	i := sort.Search(len(x.slots), func(i int) bool {
		s := x.slots[i]
		return s.off > off || (s.off == off && s.part >= part)
	})
	if i < len(x.slots) && x.slots[i].off == off && x.slots[i].part == part {
		return i, true
	}
	return 0, false
}

// page reads the entries q selects from f.
func (x *index) page(f *os.File, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	n := len(x.slots)
	start, end := max(0, n-q.Limit), n
	switch {
	case q.Before != "":
		i, ok := x.find(q.Before)
		if !ok {
			return ports.TranscriptPage{}, badCursor()
		}
		start, end = max(0, i-q.Limit), i
	case q.After != "":
		i, ok := x.find(q.After)
		if !ok {
			return ports.TranscriptPage{}, badCursor()
		}
		start, end = i+1, min(n, i+1+q.Limit)
	}
	p := ports.TranscriptPage{Entries: []ports.TranscriptEntry{}, HasOlder: start > 0, HasNewer: end < n}
	var line []byte
	var lineOff int64 = -1
	var entries []mapped
	for _, s := range x.slots[start:end] {
		if s.off != lineOff {
			if cap(line) < int(s.n) {
				line = make([]byte, s.n)
			}
			line = line[:s.n]
			if _, err := f.ReadAt(line, s.off); err != nil {
				return ports.TranscriptPage{}, err
			}
			entries, lineOff = mapLine(line, nil), s.off
		}
		if int(s.part) >= len(entries) {
			continue // cannot happen for an append-only file; never index out of range
		}
		e := entries[s.part].TranscriptEntry
		e.Cursor = s.cursor()
		if e.Kind == ports.TranscriptToolResult {
			e.Tool = s.tool
		}
		p.Entries = append(p.Entries, e)
	}
	if len(p.Entries) > 0 {
		p.FirstCursor = p.Entries[0].Cursor
		p.LastCursor = p.Entries[len(p.Entries)-1].Cursor
	}
	return p, nil
}
