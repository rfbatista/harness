package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/infra/querycount"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultSlowThreshold is how long a database call may take before it is
// logged as a slow database call.
const DefaultSlowThreshold = 500 * time.Millisecond

// Option configures Open.
type Option func(*slowCalls)

// WithSlowThreshold sets how long a database call may take before it is
// logged as a slow database call. Zero or less keeps DefaultSlowThreshold.
func WithSlowThreshold(d time.Duration) Option {
	return func(s *slowCalls) {
		if d > 0 {
			s.threshold = d
		}
	}
}

// WithLogger sets where slow database calls are logged; slog.Default() when
// unset.
func WithLogger(l *slog.Logger) Option { return func(s *slowCalls) { s.logger = l } }

// slowCalls logs every database call, lock wait included, that takes longer
// than threshold: one Warn line with the operation, its duration and whether
// it waited on the lock.
type slowCalls struct {
	threshold time.Duration
	logger    *slog.Logger
}

func newSlowCalls(opts []Option) *slowCalls {
	s := &slowCalls{threshold: DefaultSlowThreshold}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *slowCalls) log(elapsed time.Duration, lockWait bool, sql string, err error) {
	l := s.logger
	if l == nil {
		l = slog.Default()
	}
	attrs := []any{"operation", operation(sql), "duration", elapsed, "lock_wait", lockWait}
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	l.Warn("slow database call", attrs...)
}

// gormLogger is GORM's default logger, minus its own slow-SQL lines, with
// slow database calls logged by s.
func (s *slowCalls) gormLogger() logger.Interface {
	return slowCallLogger{
		Interface: logger.New(callerWriter{log.New(os.Stdout, "\r\n", log.LstdFlags)}, logger.Config{
			LogLevel: logger.Warn,
			Colorful: true,
		}),
		slow: s,
	}
}

// slowCallLogger times every statement GORM runs, on the writer connection
// and the reader pool alike, and counts it toward the querycount tracker it
// is made under.
type slowCallLogger struct {
	logger.Interface
	slow *slowCalls
}

func (l slowCallLogger) LogMode(level logger.LogLevel) logger.Interface {
	return slowCallLogger{Interface: l.Interface.LogMode(level), slow: l.slow}
}

// Trace counts a statement and logs it when slower than the threshold. GORM times a create,
// update or delete from before its transaction begins, so the time includes
// the wait for the write lock: when that wait is most of it, the call is
// logged as a lock wait. A call its caller gave up on (a request that ended)
// is not reported as failed: it was cut short.
func (l slowCallLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	querycount.Add(ctx)
	if !errors.Is(err, context.Canceled) || ctx.Err() == nil {
		l.Interface.Trace(ctx, begin, fc, err)
	}
	elapsed := time.Since(begin)
	if elapsed <= l.slow.threshold {
		return
	}
	lockWait := isLockError(err)
	if w, ok := ctx.Value(beginWaitKey{}).(*beginWait); ok && w.d >= elapsed/2 {
		lockWait = true
	}
	sql, _ := fc()
	l.slow.log(elapsed, lockWait, sql, err)
}

// callerWriter puts the caller's file:line, not this file's, in GORM's own
// log lines, which name the first frame outside GORM: slowCallLogger's.
type callerWriter struct{ logger.Writer }

func (w callerWriter) Printf(format string, args ...any) {
	if len(args) == 0 {
		w.Writer.Printf(format)
		return
	}
	if loc, ok := args[0].(string); ok && strings.Contains(loc, "/slowcalls.go:") {
		if f, ok := callerFrame(func(f runtime.Frame) bool {
			return !strings.Contains(f.File, "/gorm.io/") && !strings.HasSuffix(f.File, "/slowcalls.go")
		}); ok {
			args[0] = f.File + ":" + strconv.Itoa(f.Line)
		}
	}
	w.Writer.Printf(format, args...)
}

// beginWait carries, from a create, update or delete to the writer
// connection, how long beginning its transaction waited.
type beginWait struct{ d time.Duration }

type beginWaitKey struct{}

const beginWaitName = "harness:begin-wait"

// measureBeginWait has the creates, updates and deletes on db report how long
// their transaction waited to begin, for slowCallLogger.
func measureBeginWait(db *gorm.DB) error {
	mark := func(db *gorm.DB) {
		db.Statement.Context = context.WithValue(db.Statement.Context, beginWaitKey{}, &beginWait{})
	}
	cb := db.Callback()
	if err := cb.Create().Before("gorm:begin_transaction").Register(beginWaitName, mark); err != nil {
		return err
	}
	if err := cb.Update().Before("gorm:begin_transaction").Register(beginWaitName, mark); err != nil {
		return err
	}
	return cb.Delete().Before("gorm:begin_transaction").Register(beginWaitName, mark)
}

// writerConn is the writer connection as GORM sees it. Beginning a
// transaction waits for the writer connection and, for a file database, the
// write lock (transactions begin immediate): a slow begin is a lock wait.
type writerConn struct {
	*sql.DB
	slow *slowCalls
}

// BeginTx begins a transaction. The wait of a create, update or delete's
// transaction goes to its statement's line (slowCallLogger.Trace); any other
// transaction's slow begin is logged here.
func (c *writerConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	start := time.Now()
	tx, err := c.DB.BeginTx(ctx, opts)
	elapsed := time.Since(start)
	if w, ok := ctx.Value(beginWaitKey{}).(*beginWait); ok {
		w.d = elapsed
	} else if elapsed > c.slow.threshold {
		c.slow.log(elapsed, true, "BEGIN", err)
	}
	return tx, err
}

// GetDBConn lets gorm.DB.DB() return the writer connection's pool.
func (c *writerConn) GetDBConn() (*sql.DB, error) { return c.DB, nil }

// isLockError reports whether err is SQLite giving up on a lock
// (SQLITE_BUSY or SQLITE_LOCKED).
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}

var (
	pkgPath    = reflect.TypeOf(slowCalls{}).PkgPath()
	funcSuffix = regexp.MustCompile(`(\.func\d+|\.\d+)+$`)
	tableWord  = regexp.MustCompile("(?i)\\b(?:FROM|INTO|UPDATE|TABLE)\\s+[`\"]?(\\w+)")
)

// operation names the database call being logged: the function of this
// package (a repository method, e.g. "SessionRepository.AppendEvent") it was
// made from, else the SQL verb and table.
func operation(sql string) string {
	if f, ok := callerFrame(func(f runtime.Frame) bool {
		return strings.HasPrefix(f.Function, pkgPath+".") && !strings.HasSuffix(f.File, "/slowcalls.go")
	}); ok {
		name := funcSuffix.ReplaceAllString(strings.TrimPrefix(f.Function, pkgPath+"."), "")
		return strings.NewReplacer("(*", "", ")", "").Replace(name)
	}
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "unknown"
	}
	verb := strings.ToUpper(fields[0])
	if m := tableWord.FindStringSubmatch(sql); m != nil {
		return verb + " " + m[1]
	}
	return verb
}

// callerFrame is the innermost frame of the calling goroutine that match
// accepts.
func callerFrame(match func(runtime.Frame) bool) (runtime.Frame, bool) {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		if match(f) {
			return f, true
		}
		if !more {
			return runtime.Frame{}, false
		}
	}
}
