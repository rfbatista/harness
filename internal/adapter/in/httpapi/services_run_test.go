package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/application/services"
)

// Running a command service through the HTTP API: a real `sh -c` under the
// command runner, its status changes seen on the services feed and its
// output on the log feed.

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *servicesFixture) command(name, line string) string {
	f.t.Helper()
	in := commandInput(name)
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": line}
	return f.createID(in)
}

func (f *servicesFixture) createID(in map[string]any) string {
	f.t.Helper()
	code, out := f.create(in)
	if code != 200 {
		f.t.Fatalf("create = %d %v", code, out)
	}
	return out["service"].(map[string]any)["id"].(string)
}

func (f *servicesFixture) operate(verb, id string) (int, map[string]any) {
	f.t.Helper()
	return f.call("POST", "/api/"+verb, map[string]any{"service_id": id})
}

func (f *servicesFixture) service(id string) map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/api/get_service?service_id="+id, nil)
	if code != 200 {
		f.t.Fatalf("get_service = %d %v", code, out)
	}
	return out["service"].(map[string]any)
}

func (f *servicesFixture) status(id string) string { return f.service(id)["status"].(string) }

func (f *servicesFixture) awaitStatus(id, want string) map[string]any {
	f.t.Helper()
	eventually(f.t, "status "+want, func() bool { return f.status(id) == want })
	return f.service(id)
}

func (f *servicesFixture) readLog(id, query string) map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/api/read_service_log?service_id="+id+query, nil)
	if code != 200 {
		f.t.Fatalf("read_service_log = %d %v", code, out)
	}
	return out
}

func (f *servicesFixture) logText(id string) string {
	return f.readLog(id, "&offset=0")["text"].(string)
}

func (f *servicesFixture) awaitLog(id, substr string) string {
	f.t.Helper()
	var text string
	eventually(f.t, "log to hold "+substr, func() bool { text = f.readLog(id, "")["text"].(string); return strings.Contains(text, substr) })
	return text
}

func (f *servicesFixture) repoDir() string {
	f.t.Helper()
	r, err := f.cat.Projects.GetRepository(context.Background(), f.repoID)
	if err != nil {
		f.t.Fatal(err)
	}
	return r.RootDir
}

// serviceChanges reads n service_changed events of one service from the feed,
// skipping the others. It is the services feed filtered to one service's changes.
func serviceChanges(t *testing.T, events <-chan map[string]any, serviceID string, n int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for len(out) < n {
		e := nextEvent(t, events)
		if svc, ok := e["service"].(map[string]any); ok && svc["id"] == serviceID {
			out = append(out, e)
		}
	}
	return out
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func pidIn(t *testing.T, text, label string) int {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, label+"="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil {
				return n
			}
		}
	}
	t.Fatalf("no %s= line in %q", label, text)
	return 0
}

func TestServicesRun_ACommandThatEndsExitsWithItsCodeAndLogsItsStory(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("web", "echo hello; echo oops >&2; exit 3")
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	code, out := f.operate("start_service", id)
	if code != 200 || out["already_running"] != false {
		t.Fatalf("start = %d %v", code, out)
	}
	run := out["service"].(map[string]any)["run"].(map[string]any)
	if run["id"] == "" || run["started_by"].(map[string]any)["kind"] != "person" || run["started_by"].(map[string]any)["task_id"] != "" || run["sandboxed"] != false {
		t.Fatalf("run = %v", run)
	}
	where := run["where"].(map[string]any)
	if where["kind"] != "main" || where["dir"] != f.repoDir() {
		t.Fatalf("a start runs in the repository's main checkout: %v", where)
	}

	changes := serviceChanges(t, events, id, 3)
	var statuses []string
	for _, e := range changes {
		svc := e["service"].(map[string]any)
		statuses = append(statuses, svc["status"].(string))
		want := float64(1)
		if svc["status"] == "exited" {
			want = 0
		}
		if e["kind"] != "service_changed" || e["running_count"] != want {
			t.Fatalf("event %v: want running_count %v", e, want)
		}
	}
	if strings.Join(statuses, ",") != "starting,running,exited" {
		t.Fatalf("statuses = %v", statuses)
	}
	done := f.service(id)
	exited := done["run"].(map[string]any)
	if done["status"] != "exited" || exited["exit_code"] != float64(3) || exited["ended_at"] == "" || exited["error"] != "" {
		t.Fatalf("service = %v", done)
	}

	text := f.awaitLog(id, "exited with code 3")
	if !strings.HasPrefix(text, "[harness] command: echo hello; echo oops >&2; exit 3\n[harness] directory: "+f.repoDir()+"\n[harness] not sandboxed\n") ||
		!strings.Contains(text, "hello\n") || !strings.Contains(text, "oops\n") || !strings.HasSuffix(text, "[harness] exited with code 3\n") {
		t.Fatalf("log = %q", text)
	}

	code, list := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if code != 200 || list["services"].([]any)[0].(map[string]any)["status"] != "exited" {
		t.Fatalf("list_services = %d %v", code, list)
	}
}

func TestServicesRun_OneRunPerServiceAndManyServicesAtOnce(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	a, b := f.command("a", "sleep 60"), f.command("b", "sleep 60")

	type answer struct {
		code int
		out  map[string]any
	}
	answers := make(chan answer, 4)
	for range 4 {
		go func() {
			code, out := f.operate("start_service", a)
			answers <- answer{code, out}
		}()
	}
	runIDs, fresh := map[string]bool{}, 0
	for range 4 {
		ans := <-answers
		if ans.code != 200 {
			t.Fatalf("start = %d %v", ans.code, ans.out)
		}
		runIDs[ans.out["service"].(map[string]any)["run"].(map[string]any)["id"].(string)] = true
		if ans.out["already_running"] == false {
			fresh++
		}
	}
	if len(runIDs) != 1 || fresh != 1 {
		t.Fatalf("racing starts must share one run: runs %v, %d started it", runIDs, fresh)
	}

	if code, out := f.operate("start_service", b); code != 200 || out["already_running"] != false {
		t.Fatalf("a second service runs beside the first: %d %v", code, out)
	}
	if f.status(a) != "running" || f.status(b) != "running" {
		t.Fatal("both services run at once")
	}
	if n, _ := f.svc.RunningServiceCount(context.Background(), f.projectID); n != 2 {
		t.Fatalf("running count = %d", n)
	}
	_, again := f.operate("start_service", a)
	if again["already_running"] != true || again["service"].(map[string]any)["status"] != "running" {
		t.Fatalf("starting a running service shows it: %v", again)
	}

	for _, id := range []string{a, b} {
		code, out := f.operate("stop_service", id)
		if code != 200 || out["service"].(map[string]any)["status"] != "stopped" {
			t.Fatalf("stop = %d %v", code, out)
		}
	}
}

func TestServicesRun_StopEndsTheWholeTreeAndAStoppedServiceStaysUnchanged(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("tree", "sleep 60 & echo child=$!; echo shell=$$; wait")
	f.operate("start_service", id)
	text := f.awaitLog(id, "\nshell=")
	child, shell := pidIn(t, text, "child"), pidIn(t, text, "shell")

	code, out := f.operate("stop_service", id)
	if code != 200 {
		t.Fatalf("stop = %d %v", code, out)
	}
	stopped := out["service"].(map[string]any)
	if stopped["status"] != "stopped" || stopped["run"].(map[string]any)["exit_code"] != nil || stopped["run"].(map[string]any)["ended_at"] == "" {
		t.Fatalf("a stopped run has no exit code: %v", stopped)
	}
	eventually(t, "the whole tree to be gone", func() bool { return !alive(child) && !alive(shell) })
	if !strings.HasSuffix(f.logText(id), "[harness] stopped\n") {
		t.Fatalf("the log says how it ended: %q", f.logText(id))
	}

	_, again := f.operate("stop_service", id)
	if again["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stopping a stopped service = %v", again)
	}
	never := f.command("never", "true")
	_, out = f.operate("stop_service", never)
	if out["service"].(map[string]any)["status"] != "stopped" || out["service"].(map[string]any)["run"] != nil {
		t.Fatalf("a service that never ran stays as it is: %v", out)
	}
}

func TestServicesRun_AProcessThatIgnoresSigtermIsKilledAfterTheGrace(t *testing.T) {
	f := newServicesFixture(t, services.Options{StopGrace: 200 * time.Millisecond})
	id := f.command("stubborn", "trap '' TERM; echo pid=$$; while true; do sleep 0.05; done")
	f.operate("start_service", id)
	pid := pidIn(t, f.awaitLog(id, "\npid="), "pid")
	// The trap is set before the pid is printed; the loop keeps the shell busy.

	began := time.Now()
	code, out := f.operate("stop_service", id)
	if code != 200 || out["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop = %d %v", code, out)
	}
	if took := time.Since(began); took < 150*time.Millisecond || took > 5*time.Second {
		t.Fatalf("the stop waited the grace then killed: took %v", took)
	}
	eventually(t, "the process to be gone", func() bool { return !alive(pid) })
}

func TestServicesRun_RestartStopsThenStartsAndClearsTheLog(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("web", "echo pid=$$; exec sleep 60")
	f.operate("start_service", id)
	first := f.awaitLog(id, "\npid=")
	firstPid := pidIn(t, first, "pid")
	firstRun := f.service(id)["run"].(map[string]any)["id"]

	logFeed := sseEvents(t, f.srv.URL+"/api/services/log?service_id="+id)
	code, out := f.operate("restart_service", id)
	if code != 200 || out["service"].(map[string]any)["status"] != "running" {
		t.Fatalf("restart = %d %v", code, out)
	}
	newRun := out["service"].(map[string]any)["run"].(map[string]any)["id"]
	if newRun == firstRun {
		t.Fatal("a restart is a new run")
	}
	eventually(t, "the old process to end", func() bool { return !alive(firstPid) })

	var reset map[string]any
	for reset == nil {
		if e := nextEvent(t, logFeed); e["kind"] == "reset" {
			reset = e
		}
	}
	if reset["run_id"] != newRun {
		t.Fatalf("reset = %v, want run %v", reset, newRun)
	}
	text := f.awaitLog(id, "\npid=")
	if strings.Count(text, "[harness] command:") != 1 || strings.Contains(text, "[harness] stopped") || pidIn(t, text, "pid") == firstPid {
		t.Fatalf("every start clears the log: %q", text)
	}
	f.operate("stop_service", id)
}

func TestServicesRun_AMissingWorkingDirectoryFailsWithAOneLineError(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	sub := filepath.Join(f.repoDir(), "no/such/dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "true", "subdir": "no/such/dir"}
	id := f.createID(in)
	// The folder goes away after the save.
	if err := os.RemoveAll(filepath.Join(f.repoDir(), "no")); err != nil {
		t.Fatal(err)
	}

	code, out := f.operate("start_service", id)
	if code != 200 {
		t.Fatalf("a failure after the run was created is not a refusal: %d %v", code, out)
	}
	svc := out["service"].(map[string]any)
	run := svc["run"].(map[string]any)
	want := `subfolder "no/such/dir" does not exist in ` + f.repoDir()
	if svc["status"] != "failed" || run["error"] != want || run["exit_code"] != nil || run["ended_at"] == "" {
		t.Fatalf("service = %v", svc)
	}
	if n, _ := f.svc.RunningServiceCount(context.Background(), f.projectID); n != 0 {
		t.Fatalf("a failed run does not count: %d", n)
	}
	if !strings.HasSuffix(f.logText(id), "[harness] failed: "+want+"\n") {
		t.Fatalf("log = %q", f.logText(id))
	}

	// A failed service starts again.
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, out = f.operate("start_service", id)
	if out["already_running"] != false {
		t.Fatalf("restart of a failed service = %v", out)
	}
	f.awaitStatus(id, "exited")
}

// A command typed into the subfolder field (the reported bug) is refused at
// the save, not at every start.
func TestServicesRun_ASubfolderThatIsNotAFolderIsRefusedAtSave(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	if err := os.MkdirAll(filepath.Join(f.repoDir(), "cmd/web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repoDir(), "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	input := func(name, sub string) map[string]any {
		in := commandInput(name)
		in["repository_id"] = f.repoID
		in["command"] = map[string]any{"line": "true", "subdir": sub}
		return in
	}

	for _, sub := range []string{"npm run dev", "afile", "no/such"} {
		code, out := f.create(input("bad", sub))
		c, msg := errorOf(out)
		want := `command.subdir: "` + sub + `" is not a folder in api's main checkout`
		if code != 400 || c != "INVALID_INPUT" || msg != want {
			t.Fatalf("create with %q = %d %v", sub, code, out)
		}
	}
	if _, out := f.call("GET", "/api/list_services?project_id="+f.projectID, nil); len(out["services"].([]any)) != 0 {
		t.Fatalf("a refused save stores nothing: %v", out)
	}

	for _, sub := range []string{"", "cmd/web"} {
		if code, out := f.create(input("ok-"+strings.ReplaceAll(sub, "/", "-"), sub)); code != 200 {
			t.Fatalf("create with %q = %d %v", sub, code, out)
		}
	}

	id := f.createID(input("edit", "cmd"))
	code, out := f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": input("edit", "npm run dev")})
	if c, _ := errorOf(out); code != 400 || c != "INVALID_INPUT" {
		t.Fatalf("update to a missing subfolder = %d %v", code, out)
	}
	if got := f.service(id)["command"].(map[string]any)["subdir"]; got != "cmd" {
		t.Fatalf("a refused update changes nothing: %v", got)
	}

	// No repository: nothing to check.
	loose := commandInput("loose")
	loose["command"] = map[string]any{"line": "true", "subdir": "whatever"}
	if code, out := f.create(loose); code != 200 {
		t.Fatalf("no repository = %d %v", code, out)
	}
}

func TestServicesRun_AServiceWithNoRepositoryRunsInItsOwnFolder(t *testing.T) {
	folders := t.TempDir()
	f := newServicesFixture(t, services.Options{FolderDir: folders})
	id := f.createID(map[string]any{"name": "solo", "kind": "command", "command": map[string]any{"line": "pwd; touch made"}})

	_, out := f.operate("start_service", id)
	where := out["service"].(map[string]any)["run"].(map[string]any)["where"].(map[string]any)
	dir := filepath.Join(folders, id)
	if where["kind"] != "service_folder" || where["dir"] != dir {
		t.Fatalf("where = %v", where)
	}
	f.awaitStatus(id, "exited")
	if !strings.Contains(f.logText(id), dir+"\n") {
		t.Fatalf("the command ran in its folder: %q", f.logText(id))
	}
	if _, err := os.Stat(filepath.Join(dir, "made")); err != nil {
		t.Fatalf("the folder is the service's own: %v", err)
	}
}

func TestServicesRun_ARunningServiceCannotBeDeletedAndAnEditNeedsARestart(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("web", "sleep 60")
	f.operate("start_service", id)

	code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id})
	if c, _ := errorOf(out); code != 409 || c != "SERVICE_RUNNING" {
		t.Fatalf("delete of a running service = %d %v", code, out)
	}
	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": map[string]any{
		"name": "web", "kind": "command", "repository_id": f.repoID, "command": map[string]any{"line": "echo edited"},
	}})
	if code != 200 || out["restart_needed"] != true {
		t.Fatalf("update of a running service = %d %v", code, out)
	}
	f.operate("stop_service", id)
	if f.status(id) != "stopped" {
		t.Fatal("not stopped")
	}
	// The edit applies at the next start.
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	if !strings.Contains(f.logText(id), "[harness] command: echo edited\n") {
		t.Fatalf("log = %q", f.logText(id))
	}
	if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id}); code != 200 {
		t.Fatalf("delete of an ended service = %d %v", code, out)
	}
}

func TestServicesRun_LogReadsByOffset(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("big", "head -c 600000 /dev/zero | tr '\\0' x; printf '\\377\\376ok'; echo")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	f.awaitLog(id, "exited with code 0")

	whole := f.readLog(id, "&offset=0")
	if len(whole["text"].(string)) != 256<<10 || whole["next_offset"] != float64(256<<10) || whole["offset"] != float64(0) || whole["first_offset"] != float64(0) {
		t.Fatalf("one answer holds at most 256 KB: offset %v next %v len %d", whole["offset"], whole["next_offset"], len(whole["text"].(string)))
	}
	runID := f.service(id)["run"].(map[string]any)["id"]
	if whole["run_id"] != runID {
		t.Fatalf("run_id = %v, want %v", whole["run_id"], runID)
	}

	tail := f.readLog(id, "")
	text := tail["text"].(string)
	if !strings.HasSuffix(text, "��ok\n[harness] exited with code 0\n") {
		t.Fatalf("invalid bytes become replacement characters: %q", text[len(text)-60:])
	}
	if end := tail["next_offset"].(float64); tail["offset"].(float64) != end-65536 {
		t.Fatalf("an omitted offset reads the last 64 KB: offset %v end %v", tail["offset"], end)
	}

	// Reading on from next_offset walks the log to its end.
	end := tail["next_offset"].(float64)
	past := f.readLog(id, "&offset="+strconv.Itoa(int(end)+1000))
	if past["text"] != "" || past["next_offset"] != end {
		t.Fatalf("an offset past the end reads nothing: %v", past)
	}

	if code, out := f.call("GET", "/api/read_service_log?service_id="+id+"&offset=-1", nil); code != 400 {
		t.Fatalf("a negative offset = %d %v", code, out)
	}
}

func TestServicesRun_LogKeepsOnlyTheLastBytesWithMonotonicOffsets(t *testing.T) {
	f := newServicesFixture(t, services.Options{LogCap: 1000})
	// 25 lines of 100 bytes: 2500 bytes, then the [harness] end line.
	id := f.command("noisy", "i=0; while [ $i -lt 25 ]; do printf '%099d\\n' $i; i=$((i+1)); done")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	f.awaitLog(id, "exited with code 0")

	read := f.readLog(id, "&offset=0")
	first, next, text := read["first_offset"].(float64), read["next_offset"].(float64), read["text"].(string)
	if len(text) != 1000 || first != next-1000 || first == 0 || read["offset"] != first {
		t.Fatalf("only the last 1000 bytes are kept, offsets keep counting: first %v next %v len %d", first, next, len(text))
	}
	if !strings.HasSuffix(text, "[harness] exited with code 0\n") {
		t.Fatalf("log = %q", text)
	}
	at := f.readLog(id, "&offset="+strconv.Itoa(int(first)+500))
	if at["text"] != text[500:] || at["next_offset"] != next {
		t.Fatalf("a read inside the kept bytes starts there: %v", at)
	}
}

func TestServicesRun_LogFeedSendsTheLogFromAnOffsetThenEachChunk(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	gate := filepath.Join(t.TempDir(), "go")
	id := f.command("slow", "echo one; while [ ! -e "+gate+" ]; do sleep 0.02; done; echo two")
	f.operate("start_service", id)
	f.awaitLog(id, "one\n")

	// From the start: what is there, then what follows.
	feed := sseEvents(t, f.srv.URL+"/api/services/log?service_id="+id+"&from=0")
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runID := f.service(id)["run"].(map[string]any)["id"]
	var got strings.Builder
	var want float64
	for !strings.Contains(got.String(), "exited with code 0") {
		e := nextEvent(t, feed)
		if e["kind"] != "chunk" || e["run_id"] != runID || e["offset"] != want {
			t.Fatalf("chunks follow one another from offset 0: %v at %v", e, want)
		}
		got.WriteString(e["text"].(string))
		want += float64(len(e["text"].(string)))
	}
	if got.String() != f.logText(id) {
		t.Fatalf("feed = %q, log = %q", got.String(), f.logText(id))
	}

	// With no from: only what comes next, on the tab's stream too.
	end := f.readLog(id, "")["next_offset"].(float64)
	tail := sseEvents(t, f.srv.URL+"/api/services/log?service_id="+id+"&from="+strconv.Itoa(int(end)))
	f.operate("start_service", id)
	if e := nextEvent(t, tail); e["kind"] != "reset" || e["run_id"] == runID {
		t.Fatalf("a new run resets the log feed: %v", e)
	}
	if e := nextEvent(t, tail); e["kind"] != "chunk" || e["offset"] != float64(0) {
		t.Fatalf("the new run's first chunk is at 0: %v", e)
	}
}

func TestServicesRun_LogFeedOnTheTabsStream(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("web", "echo hi")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	f.awaitLog(id, "exited with code 0")

	stream := sseEvents(t, f.srv.URL+"/api/stream")
	streamID := nextEvent(t, stream)["stream"].(string)
	code, out := f.call("POST", "/api/stream/follow", map[string]any{"stream": streamID, "path": "/services/log?service_id=" + id + "&from=0"})
	if code != 200 {
		t.Fatalf("follow = %d %v", code, out)
	}
	var got strings.Builder
	for !strings.Contains(got.String(), "exited with code 0") {
		e := nextEvent(t, stream)
		data, _ := e["data"].(map[string]any)
		if data["kind"] != "chunk" {
			t.Fatalf("event = %v", e)
		}
		got.WriteString(data["text"].(string))
	}
}

func TestServicesRun_EnvFollowsThePrecedenceAndStaysOutOfTheLogLines(t *testing.T) {
	t.Setenv("HARNESS_TEST_SERVER_SECRET", "server-secret-value")
	f := newServicesFixture(t, services.Options{})
	ctx := context.Background()
	if _, err := f.cat.Projects.SaveEnvFile(ctx, f.repoID, ".env", "A=from-dotenv\nB=from-dotenv\n"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(f.repoDir())

	run := func(name string, export bool) string {
		in := commandInput(name)
		in["repository_id"] = f.repoID
		in["export_repository_env"] = export
		in["env"] = "B=from-service\nC=service-only-secret"
		in["command"] = map[string]any{"line": `echo "A=${A-unset} B=$B C=$C S=${HARNESS_TEST_SERVER_SECRET-unset}"`}
		id := f.createID(in)
		f.operate("start_service", id)
		f.awaitStatus(id, "exited")
		return f.awaitLog(id, "exited with code 0")
	}

	exported := run("exported", true)
	if !strings.Contains(exported, "A=from-dotenv B=from-service C=service-only-secret S=unset\n") {
		t.Fatalf("service env beats the repository's .env, which beats the base; the server's own env is not passed: %q", exported)
	}
	plain := run("plain", false)
	if !strings.Contains(plain, "A=unset B=from-service C=service-only-secret S=unset\n") {
		t.Fatalf("the .env is exported only when asked: %q", plain)
	}
	for _, log := range []string{exported, plain} {
		for _, line := range strings.Split(log, "\n") {
			if strings.HasPrefix(line, "[harness] ") && (strings.Contains(line, "from-dotenv") || strings.Contains(line, "from-service") || strings.Contains(line, "service-only-secret")) {
				t.Fatalf("an env value is in a [harness] line: %q", line)
			}
		}
	}
	after, _ := os.ReadDir(f.repoDir())
	if len(after) != len(before) {
		t.Fatalf("a run writes no env file into the checkout: %v -> %v", before, after)
	}
}

func TestServicesRun_TheLogAndLastRunSurviveANewModuleOnTheSameDataDirectory(t *testing.T) {
	f := newServicesFixture(t, services.Options{LogCap: 1000})
	id := f.command("noisy", "i=0; while [ $i -lt 25 ]; do printf '%099d\\n' $i; i=$((i+1)); done; exit 4")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	f.awaitLog(id, "exited with code 4")
	want := f.readLog(id, "&offset=0")

	again := services.New(f.store, f.cat.Projects, f.cat.Projects, services.Options{LogDir: f.logDir, LogCap: 1000})
	read, err := again.ReadServiceLog(context.Background(), id, ptr(int64(0)))
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != want["text"] || float64(read.FirstOffset) != want["first_offset"] || float64(read.NextOffset) != want["next_offset"] || read.RunID != want["run_id"] {
		t.Fatalf("a new instance reads the same log: %+v vs %v", read, want)
	}
	svc, err := again.GetService(context.Background(), id)
	if err != nil || svc.Status != "exited" || svc.Run == nil || svc.Run.ExitCode == nil || *svc.Run.ExitCode != 4 {
		t.Fatalf("the last run survives: %+v %v", svc, err)
	}
}

func TestServicesRun_UnknownServiceAndFeedErrors(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	for _, verb := range []string{"start_service", "stop_service", "restart_service"} {
		code, out := f.operate(verb, "nope")
		if c, _ := errorOf(out); code != 404 || c != "SERVICE_NOT_FOUND" {
			t.Fatalf("%s of an unknown service = %d %v", verb, code, out)
		}
	}
	if code, out := f.call("GET", "/api/read_service_log?service_id=nope", nil); code != 404 {
		t.Fatalf("read of an unknown service = %d %v", code, out)
	}
	res, err := http.Get(f.srv.URL + "/api/services/log?service_id=nope")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("log feed of an unknown service = %d", res.StatusCode)
	}
}

// The tail read the agents' tool uses: the last lines, with an offset to
// follow on from.
func TestServicesRun_LogTailReadsTheLastLines(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.command("lines", "for i in 1 2 3 4 5; do echo line$i; done")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	f.awaitLog(id, "exited with code 0")

	read, truncated, err := f.svc.ReadServiceLogTail(context.Background(), id, 3)
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != "line4\nline5\n[harness] exited with code 0\n" || truncated {
		t.Fatalf("tail = %q truncated %v", read.Text, truncated)
	}
	rest, err := f.svc.ReadServiceLog(context.Background(), id, ptr(read.Offset))
	if err != nil || rest.Text != read.Text || rest.NextOffset != read.NextOffset {
		t.Fatalf("the tail's offset reads the same text: %+v %v", rest, err)
	}
}

func ptr[T any](v T) *T { return &v }
