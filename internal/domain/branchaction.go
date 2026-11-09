package domain

import "strings"

// BranchActionKind is what an agent session does to a branch or a remote
// (Glossary "Branch action"; contract "Harness server ↔ Agent sessions —
// branch gate" §1).
type BranchActionKind string

const (
	BranchCreate       BranchActionKind = "create"
	BranchSwitch       BranchActionKind = "switch"
	BranchRename       BranchActionKind = "rename"
	BranchDelete       BranchActionKind = "delete"
	BranchPush         BranchActionKind = "push"
	BranchRemoteAdd    BranchActionKind = "remote_add"
	BranchRemoteSetURL BranchActionKind = "remote_set_url"
	BranchRemoteRename BranchActionKind = "remote_rename"
	BranchRemoteRemove BranchActionKind = "remote_remove"
)

// Gated says whether the branch gate asks the person about the kind: every
// branch action, every push and every change of where a push goes (ADR 0071).
func (k BranchActionKind) Gated() bool {
	switch k {
	case BranchCreate, BranchSwitch, BranchRename, BranchDelete, BranchPush,
		BranchRemoteAdd, BranchRemoteSetURL, BranchRemoteRename, BranchRemoteRemove:
		return true
	}
	return false
}

// BranchStart is where a created or switched-to branch starts from: a branch
// (Kind "branch", Ref its name) or a commit (Kind "commit").
type BranchStart struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Commit string `json:"commit,omitempty"`
}

// BranchAction is one branch action found in a git command.
type BranchAction struct {
	Kind BranchActionKind `json:"action"`
	// Branch is the branch acted on; for a rename its old name.
	Branch  string `json:"branch"`
	NewName string `json:"new_name,omitempty"`
	// Start is set for create and switch.
	Start *BranchStart `json:"start_point"`
	// Remote, URL and Refspec describe a push or a remote action. For a push
	// Branch is the local branch, RemoteBranch the branch it lands on, URL the
	// remote's URL as configured, and Refspec what the agent typed.
	Remote       string `json:"remote,omitempty"`
	URL          string `json:"url,omitempty"`
	Refspec      string `json:"refspec,omitempty"`
	RemoteBranch string `json:"remote_branch,omitempty"`
	// Commit is the commit a push_branch request pins, "" otherwise.
	Commit string `json:"-"`
	// Dir is the directory git runs in, set by the gate for what it reads of
	// the repository.
	Dir string `json:"-"`
	// Refused is the reason the action is refused outright; "" when it is
	// not.
	Refused string `json:"-"`
}

// GitProbe answers what a classifier must know of the repository to read git's
// own arguments the way git does. Every method takes the directory git runs in.
type GitProbe interface {
	// Commit resolves ref to a commit id; "" when it is none.
	Commit(dir, ref string) string
	// Branch says whether refs/heads/<name> exists.
	Branch(dir, name string) bool
	// RemoteBranch says whether exactly one remote has a branch of that name.
	RemoteBranch(dir, name string) (remote string, ok bool)
	// Path says whether the working tree has a file or folder of that name.
	Path(dir, p string) bool
	// Current is the checked-out branch; "" when HEAD is detached.
	Current(dir string) string
	// RemoteURL is where a push to remote name goes: its pushurl when it has
	// one (several are joined by ", "), else its url; "" when there is no such
	// remote.
	RemoteURL(dir, name string) string
	// Config is every value git config holds for key, one per line; "" when
	// it has none.
	Config(dir, key string) string
	// PushRemote is where a bare `git push` of branch goes: its pushRemote,
	// its remote, remote.pushDefault, else origin.
	PushRemote(dir, branch string) string
}

// ClassifyGit turns the arguments of one git invocation (without "git") into
// the branch actions it makes: none for a command that is not one. dir is the
// directory git runs in.
func ClassifyGit(args []string, dir string, p GitProbe) []BranchAction {
	if reason := refusedOverride(args); reason != "" {
		return []BranchAction{{Kind: BranchPush, Refused: reason}}
	}
	args, dir = skipGlobalOptions(args, dir)
	if len(args) == 0 {
		return nil
	}
	c := classifier{dir: dir, p: p}
	rest := args[1:]
	switch args[0] {
	case "checkout":
		return c.checkout(rest)
	case "switch":
		return c.switchCmd(rest)
	case "branch":
		return c.branch(rest)
	case "worktree":
		return c.worktree(rest)
	case "stash":
		if len(rest) >= 2 && rest[0] == "branch" {
			return []BranchAction{c.create(rest[1], c.stashStart())}
		}
	case "rebase":
		return c.rebase(rest)
	case "push":
		return c.push(rest)
	case "remote":
		return c.remote(rest)
	case "config":
		return c.config(rest)
	}
	return nil
}

// refusedConfigKey says whether a one-off `git -c key=value` changes where or how
// a push goes: a remote's URL, push URL or push refspec, or a URL rewrite.
func refusedConfigKey(key string) bool {
	key = strings.ToLower(key)
	if strings.HasPrefix(key, "url.") {
		return true
	}
	if !strings.HasPrefix(key, "remote.") {
		return false
	}
	switch key[strings.LastIndex(key, ".")+1:] {
	case "url", "pushurl", "push":
		return true
	}
	return false
}

const refusedOverrideReason = "REFUSED: git -c cannot change a remote's URL, push URL or push refspec for one command. Ask for the change with git remote set-url or git config, which waits for the person."

// refusedOverride is the reason a git invocation is refused for a `-c` that
// redirects pushes, "" when it has none.
func refusedOverride(args []string) string {
	for i := 0; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		a := args[i]
		switch {
		case a == "-c" && i+1 < len(args):
			key, _, _ := strings.Cut(args[i+1], "=")
			if refusedConfigKey(key) {
				return refusedOverrideReason
			}
			i++
		case a == "-C" || a == "--git-dir" || a == "--work-tree" || a == "--namespace":
			i++
		case strings.HasPrefix(a, "-c") && len(a) > 2 && a[1] == 'c':
			key, _, _ := strings.Cut(a[2:], "=")
			if refusedConfigKey(key) {
				return refusedOverrideReason
			}
		}
	}
	return ""
}

func skipGlobalOptions(args []string, dir string) ([]string, string) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		a := args[0]
		switch {
		case a == "-C" && len(args) > 1:
			dir = joinDir(dir, args[1])
			args = args[2:]
		case (a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace") && len(args) > 1:
			args = args[2:]
		default:
			args = args[1:]
		}
	}
	return args, dir
}

func joinDir(dir, next string) string {
	if strings.HasPrefix(next, "/") || dir == "" {
		return next
	}
	return strings.TrimSuffix(dir, "/") + "/" + next
}

type classifier struct {
	dir string
	p   GitProbe
}

func (c classifier) start(ref string) *BranchStart {
	if ref == "" {
		if cur := c.p.Current(c.dir); cur != "" {
			return &BranchStart{Kind: "branch", Ref: cur, Commit: c.p.Commit(c.dir, "HEAD")}
		}
		return &BranchStart{Kind: "commit", Ref: "HEAD", Commit: c.p.Commit(c.dir, "HEAD")}
	}
	if c.p.Branch(c.dir, ref) {
		return &BranchStart{Kind: "branch", Ref: ref, Commit: c.p.Commit(c.dir, ref)}
	}
	return &BranchStart{Kind: "commit", Ref: ref, Commit: c.p.Commit(c.dir, ref)}
}

func (c classifier) stashStart() *BranchStart { return c.start("") }

func (c classifier) create(name string, start *BranchStart) BranchAction {
	return BranchAction{Kind: BranchCreate, Branch: name, Start: start}
}

func (c classifier) switchTo(name string) []BranchAction {
	if name == c.p.Current(c.dir) {
		return nil
	}
	if c.p.Branch(c.dir, name) {
		return []BranchAction{{Kind: BranchSwitch, Branch: name, Start: c.start(name)}}
	}
	if remote, ok := c.p.RemoteBranch(c.dir, name); ok {
		return []BranchAction{c.create(name, c.start(remote+"/"+name))}
	}
	if name == "-" {
		return []BranchAction{{Kind: BranchSwitch, Branch: "-"}}
	}
	if commit := c.p.Commit(c.dir, name); commit != "" {
		return []BranchAction{{Kind: BranchSwitch, Branch: "", Start: &BranchStart{Kind: "commit", Ref: name, Commit: commit}}}
	}
	return nil
}

// optionValue reads an option that takes a value, as -b name or --orphan=name
// or -bname.
func optionValue(args []string, i int, short, long string) (value string, used int, ok bool) {
	a := args[i]
	switch {
	case a == short || a == long:
		if i+1 < len(args) {
			return args[i+1], 2, true
		}
		return "", 1, true
	case long != "" && strings.HasPrefix(a, long+"="):
		return strings.TrimPrefix(a, long+"="), 1, true
	case short != "" && len(a) > 2 && strings.HasPrefix(a, short) && !strings.HasPrefix(a, "--"):
		return a[2:], 1, true
	}
	return "", 0, false
}

func (c classifier) checkout(args []string) []BranchAction {
	var pos []string
	create, detach := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return nil // a path checkout
		}
		if a == "-p" || a == "--patch" {
			return nil
		}
		if v, n, ok := optionValue(args, i, "-b", ""); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(args, i, "-B", ""); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(args, i, "", "--orphan"); ok {
			create = v
			i += n - 1
			continue
		}
		if a == "--detach" || a == "-d" {
			detach = true
			continue
		}
		if v, n, ok := optionValue(args, i, "", "--conflict"); ok {
			_ = v
			i += n - 1
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		pos = append(pos, a)
	}
	if create != "" {
		start := ""
		if len(pos) > 0 {
			start = pos[0]
		}
		return []BranchAction{c.create(create, c.start(start))}
	}
	if len(pos) != 1 {
		return nil // no target, or a tree-ish with paths
	}
	if detach {
		return []BranchAction{{Kind: BranchSwitch, Start: c.start(pos[0])}}
	}
	if !c.p.Branch(c.dir, pos[0]) && c.p.Path(c.dir, pos[0]) {
		return nil
	}
	return c.switchTo(pos[0])
}

func (c classifier) switchCmd(args []string) []BranchAction {
	var pos []string
	create, detach := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, n, ok := optionValue(args, i, "-c", "--create"); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(args, i, "-C", "--force-create"); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(args, i, "", "--orphan"); ok {
			create = v
			i += n - 1
			continue
		}
		if a == "-d" || a == "--detach" {
			detach = true
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		pos = append(pos, a)
	}
	if create != "" {
		start := ""
		if len(pos) > 0 {
			start = pos[0]
		}
		return []BranchAction{c.create(create, c.start(start))}
	}
	if len(pos) == 0 {
		return nil
	}
	if detach {
		return []BranchAction{{Kind: BranchSwitch, Start: c.start(pos[0])}}
	}
	return c.switchTo(pos[0])
}

// branchReadOptions are the options of git branch that only list.
var branchReadOptions = map[string]bool{
	"-l": true, "--list": true, "-a": true, "--all": true, "-r": true, "--remotes": true,
	"--show-current": true, "-v": true, "-vv": true, "--verbose": true,
	"--contains": true, "--no-contains": true, "--merged": true, "--no-merged": true,
	"--points-at": true, "--format": true, "--sort": true,
}

func (c classifier) branch(args []string) []BranchAction {
	mode := ""
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-m", "-M", "--move":
			mode = "rename"
		case "-d", "-D", "--delete":
			mode = "delete"
		case "-c", "-C", "--copy":
			mode = "copy"
		case "--set-upstream-to", "-u", "--unset-upstream", "--edit-description":
			return nil
		default:
			switch {
			case branchReadOptions[a], strings.HasPrefix(a, "--format="), strings.HasPrefix(a, "--sort="), strings.HasPrefix(a, "--set-upstream-to="):
				if mode == "" && a != "-v" && a != "-vv" && a != "--verbose" {
					return nil
				}
			case a == "-f" || a == "--force" || a == "-q" || a == "--quiet" || a == "-t" || a == "--track" || a == "--no-track":
			case strings.HasPrefix(a, "-") && a != "-":
				if mode == "" && (strings.HasPrefix(a, "-d") || strings.HasPrefix(a, "-D") || strings.HasPrefix(a, "-m") || strings.HasPrefix(a, "-M")) {
					// combined short options, as -dr or -Mf
					switch a[1] {
					case 'd', 'D':
						mode = "delete"
					default:
						mode = "rename"
					}
				}
			default:
				pos = append(pos, a)
			}
		}
	}
	switch mode {
	case "rename":
		switch len(pos) {
		case 1:
			return []BranchAction{{Kind: BranchRename, Branch: c.p.Current(c.dir), NewName: pos[0]}}
		case 2:
			return []BranchAction{{Kind: BranchRename, Branch: pos[0], NewName: pos[1]}}
		}
	case "delete":
		var out []BranchAction
		for _, name := range pos {
			out = append(out, BranchAction{Kind: BranchDelete, Branch: name})
		}
		return out
	case "copy":
		switch len(pos) {
		case 1:
			return []BranchAction{c.create(pos[0], c.start(c.p.Current(c.dir)))}
		case 2:
			return []BranchAction{c.create(pos[1], c.start(pos[0]))}
		}
	default:
		switch len(pos) {
		case 1:
			return []BranchAction{c.create(pos[0], c.start(""))}
		case 2:
			return []BranchAction{c.create(pos[0], c.start(pos[1]))}
		}
	}
	return nil
}

func (c classifier) worktree(args []string) []BranchAction {
	if len(args) == 0 || args[0] != "add" {
		return nil
	}
	var pos []string
	create, detach, orphan := "", false, ""
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if v, n, ok := optionValue(rest, i, "-b", ""); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(rest, i, "-B", ""); ok {
			create = v
			i += n - 1
			continue
		}
		if v, n, ok := optionValue(rest, i, "", "--orphan"); ok {
			orphan = v
			i += n - 1
			continue
		}
		if a == "--detach" {
			detach = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		pos = append(pos, a)
	}
	if orphan != "" {
		create = orphan
	}
	if create != "" {
		start := ""
		if len(pos) > 1 {
			start = pos[1]
		}
		return []BranchAction{c.create(create, c.start(start))}
	}
	if len(pos) == 0 {
		return nil
	}
	if len(pos) == 1 {
		if detach {
			return []BranchAction{{Kind: BranchSwitch, Start: c.start("")}}
		}
		// git names the new branch after the folder
		name := pos[0][strings.LastIndex(pos[0], "/")+1:]
		if c.p.Branch(c.dir, name) {
			return []BranchAction{{Kind: BranchSwitch, Branch: name, Start: c.start(name)}}
		}
		return []BranchAction{c.create(name, c.start(""))}
	}
	if detach || !c.p.Branch(c.dir, pos[1]) {
		return []BranchAction{{Kind: BranchSwitch, Start: c.start(pos[1])}}
	}
	return []BranchAction{{Kind: BranchSwitch, Branch: pos[1], Start: c.start(pos[1])}}
}

// rebaseValueOptions take their value as the next argument.
var rebaseValueOptions = map[string]bool{"--onto": true, "-s": true, "--strategy": true, "-X": true, "--strategy-option": true, "-x": true, "--exec": true, "--whitespace": true}

func (c classifier) rebase(args []string) []BranchAction {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--continue", "--abort", "--skip", "--quit", "--edit-todo", "--show-current-patch":
			return nil
		}
		if rebaseValueOptions[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) < 2 {
		return nil
	}
	branch := pos[len(pos)-1]
	if branch == c.p.Current(c.dir) {
		return nil
	}
	if c.p.Branch(c.dir, branch) {
		return []BranchAction{{Kind: BranchSwitch, Branch: branch, Start: c.start(branch)}}
	}
	if commit := c.p.Commit(c.dir, branch); commit != "" {
		return []BranchAction{{Kind: BranchSwitch, Start: &BranchStart{Kind: "commit", Ref: branch, Commit: commit}}}
	}
	return []BranchAction{c.create(branch, c.start(pos[len(pos)-2]))}
}

const (
	refusedForce  = "REFUSED: a force push is refused. Push without --force, --force-with-lease, --force-if-includes or a + refspec."
	refusedDelete = "REFUSED: deleting a branch on a remote is refused."
	refusedBulk   = "REFUSED: --mirror, --prune, --all, --branches, --tags and --follow-tags pushes are refused. Push the branch you mean by name."
)

// refusedLongPushOptions are the long options of git push that are refused, in
// the order they are matched. git takes any unambiguous abbreviation of a long
// option, so a prefix of one of them is refused too.
var refusedLongPushOptions = []struct{ name, reason string }{
	{"--force-with-lease", refusedForce}, {"--force-if-includes", refusedForce}, {"--force", refusedForce},
	{"--delete", refusedDelete},
	{"--mirror", refusedBulk}, {"--prune", refusedBulk}, {"--all", refusedBulk}, {"--branches", refusedBulk},
	{"--tags", refusedBulk}, {"--follow-tags", refusedBulk},
}

func refusedLongPushOption(a string) string {
	name, _, _ := strings.Cut(a, "=")
	if len(name) < 3 {
		return ""
	}
	for _, o := range refusedLongPushOptions {
		if strings.HasPrefix(o.name, name) || strings.HasPrefix(name, o.name) {
			return o.reason
		}
	}
	return ""
}

func (c classifier) push(args []string) []BranchAction {
	var pos []string
	refused := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case a == "--repo" || a == "-o" || a == "--push-option" || a == "--receive-pack" || a == "--exec":
			i++
		case strings.HasPrefix(a, "--"):
			if r := refusedLongPushOption(a); r != "" {
				refused = r
			}
		case strings.HasPrefix(a, "-") && a != "-":
			// bundled short flags, as -fu or -df
			for _, f := range a[1:] {
				switch f {
				case 'f':
					refused = refusedForce
				case 'd':
					refused = refusedDelete
				case 'o':
					i++ // -o takes the next argument
				}
			}
		default:
			pos = append(pos, a)
		}
	}
	current := c.p.Current(c.dir)
	remote := ""
	if len(pos) > 0 {
		remote = pos[0]
		pos = pos[1:]
	} else {
		remote = c.p.PushRemote(c.dir, current)
	}
	url := c.p.RemoteURL(c.dir, remote)
	if url == "" && (strings.Contains(remote, "://") || strings.Contains(remote, "/") || strings.Contains(remote, "@")) {
		url = remote // git was given the URL itself
	}
	if len(pos) == 0 {
		pos = []string{""}
	}
	out := make([]BranchAction, 0, len(pos))
	for _, ref := range pos {
		act := BranchAction{Kind: BranchPush, Remote: remote, URL: url, Refspec: ref, Refused: refused}
		if ref == "" {
			// git picks the branch and where it goes: say which, or refuse.
			dst, why := c.bareDestination(remote, current)
			if why != "" && act.Refused == "" {
				act.Refused = refusedUnclear(why)
			}
			act.Branch, act.RemoteBranch = current, dst
			out = append(out, act)
			continue
		}
		if strings.Contains(ref, "*") && act.Refused == "" {
			act.Refused = refusedUnclear("the refspec " + ref + " has a pattern")
		}
		src, dst, _ := strings.Cut(ref, ":")
		switch {
		case strings.HasPrefix(ref, "+"):
			act.Refused = refusedForce
			src = strings.TrimPrefix(src, "+")
		case strings.HasPrefix(ref, ":"):
			act.Refused = refusedDelete
		}
		if src == "HEAD" || src == "" {
			src = current
		}
		src = strings.TrimPrefix(src, "refs/heads/")
		dst = strings.TrimPrefix(dst, "refs/heads/")
		if dst == "" {
			dst = src
		}
		act.Branch, act.RemoteBranch = src, dst
		out = append(out, act)
	}
	return out
}

func refusedUnclear(why string) string {
	return "REFUSED: git cannot say which branch this push sends where (" + why + "). Run it as an explicit git push <remote> <branch>:<remote-branch>."
}

// bareDestination is the remote branch a `git push` with no refspec sends the
// current branch to, the way git picks it (push.default and remote.<n>.push);
// why says so when it cannot be told for this one branch.
func (c classifier) bareDestination(remote, current string) (dst, why string) {
	if current == "" {
		return "", "HEAD is detached"
	}
	if c.p.Config(c.dir, "remote."+remote+".push") != "" {
		return "", "remote." + remote + ".push is set"
	}
	mode := strings.ToLower(strings.TrimSpace(c.p.Config(c.dir, "push.default")))
	switch mode {
	case "", "simple", "current":
		return current, ""
	case "upstream", "tracking":
		merge := strings.TrimPrefix(strings.TrimSpace(c.p.Config(c.dir, "branch."+current+".merge")), "refs/heads/")
		if merge == "" {
			return "", "push.default=upstream and " + current + " has no upstream"
		}
		return merge, ""
	}
	return "", "push.default=" + mode
}

func (c classifier) remote(args []string) []BranchAction {
	var pos []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 {
		return nil
	}
	switch pos[0] {
	case "add":
		act := BranchAction{Kind: BranchRemoteAdd, Remote: pos[1]}
		if len(pos) > 2 {
			act.URL = pos[2]
		}
		return []BranchAction{act}
	case "set-url":
		act := BranchAction{Kind: BranchRemoteSetURL, Remote: pos[1]}
		if len(pos) > 2 {
			act.URL = pos[2]
		}
		return []BranchAction{act}
	case "rename":
		act := BranchAction{Kind: BranchRemoteRename, Remote: pos[1]}
		if len(pos) > 2 {
			act.NewName = pos[2]
		}
		return []BranchAction{act}
	case "remove", "rm":
		return []BranchAction{{Kind: BranchRemoteRemove, Remote: pos[1]}}
	}
	return nil
}

// configReads are the flags of git config that only read.
var configReads = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--get-urlmatch": true,
	"-l": true, "--list": true,
}

func (c classifier) config(args []string) []BranchAction {
	var pos []string
	write := false
	for _, a := range args {
		switch {
		case configReads[a]:
			return nil
		case a == "--add" || a == "--replace-all" || a == "--unset" || a == "--unset-all" || a == "--rename-section" || a == "--remove-section":
			write = true
		case strings.HasPrefix(a, "-"):
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		return nil
	}
	key := strings.ToLower(pos[0])
	if !write && len(pos) < 2 {
		return nil // a read of one key
	}
	switch {
	case strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".url"):
		return []BranchAction{{Kind: BranchRemoteSetURL, Remote: strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url"), URL: valueAt(pos, 1)}}
	case strings.HasPrefix(key, "remote."):
		// pushurl, push, fetch, tagopt …: the setting itself is what is held, with its value.
		name := strings.TrimPrefix(key, "remote.")
		name = name[:strings.LastIndex(name, ".")]
		setting := valueAt(pos, 1)
		if !strings.HasSuffix(key, ".pushurl") {
			setting = key + "=" + setting
		}
		return []BranchAction{{Kind: BranchRemoteSetURL, Remote: name, URL: setting}}
	case strings.HasPrefix(key, "url."):
		// a URL rewrite redirects every remote it matches
		return []BranchAction{{Kind: BranchRemoteSetURL, Remote: "url", URL: key + "=" + valueAt(pos, 1)}}
	case strings.HasPrefix(key, "branch.") && (strings.HasSuffix(key, ".remote") || strings.HasSuffix(key, ".pushremote")):
		name := strings.TrimPrefix(key, "branch.")
		name = name[:strings.LastIndex(name, ".")]
		return []BranchAction{{Kind: BranchRemoteSetURL, Branch: name, Remote: valueAt(pos, 1)}}
	}
	return nil
}

func valueAt(pos []string, i int) string {
	if i < len(pos) {
		return pos[i]
	}
	return ""
}
