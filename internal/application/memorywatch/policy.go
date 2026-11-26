package memorywatch

import (
	"path/filepath"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The árvore de processos da sessão and who in it may be ended (ADR 0005). It
// is pure: a table row set and a watch in, a view and a victim out.

// tree is one session's processes in one sample.
type tree struct {
	// members are the processes counted against the orçamento: the agent CLI,
	// its descendants and every process of the PTY's POSIX session.
	members []ports.ProcessInfo
	// rss is their total.
	rss int64
	// agentRSS is what the agent CLI itself holds.
	agentRSS int64
	// eligible are the members that may be ended: not the agent CLI, nor
	// anything above it (its login shell and the sandbox wrapper).
	eligible []ports.ProcessInfo
}

// treeOf finds the session's tree in table; ok is false while the agent CLI is
// not in it, when nothing is counted or ended.
func treeOf(table []ports.ProcessInfo, w ports.MemoryWatch) (t tree, ok bool) {
	byPID := make(map[int]ports.ProcessInfo, len(table))
	children := make(map[int][]int, len(table))
	for _, p := range table {
		byPID[p.PID] = p
		children[p.PPID] = append(children[p.PPID], p.PID)
	}
	agent, ok := findAgent(table, w.ClaudeID)
	if !ok {
		return tree{}, false
	}

	// Never ended: the agent CLI and everything above it.
	protected := map[int]bool{}
	for pid, seen := agent.PID, 0; seen < len(table); seen++ {
		protected[pid] = true
		parent, found := byPID[byPID[pid].PPID]
		if !found || parent.PID == pid {
			break
		}
		pid = parent.PID
	}

	member := map[int]bool{agent.PID: true}
	for stack := []int{agent.PID}; len(stack) > 0; {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[pid] {
			if !member[c] {
				member[c] = true
				stack = append(stack, c)
			}
		}
	}
	if w.SID != 0 {
		for _, p := range table {
			if p.SID == w.SID {
				member[p.PID] = true
			}
		}
	}

	for _, p := range table {
		if !member[p.PID] {
			continue
		}
		t.members = append(t.members, p)
		t.rss += p.RSSBytes
		if p.PID == agent.PID {
			t.agentRSS = p.RSSBytes
		}
		if !protected[p.PID] {
			t.eligible = append(t.eligible, p)
		}
	}
	return t, true
}

// findAgent is the agent CLI: the process started for the conversation, the
// one nearest the root when several carry its id (a wrapper may repeat the
// argument).
func findAgent(table []ports.ProcessInfo, claudeID string) (ports.ProcessInfo, bool) {
	if claudeID == "" {
		return ports.ProcessInfo{}, false
	}
	var best ports.ProcessInfo
	found := false
	for _, p := range table {
		if p.Session == claudeID && (!found || p.PID > best.PID) {
			best, found = p, true
		}
	}
	return best, found
}

// heaviest is the eligible process holding the most RSS (the highest pid on a
// tie), if there is one.
func (t tree) heaviest() (ports.ProcessInfo, bool) {
	var best ports.ProcessInfo
	found := false
	for _, p := range t.eligible {
		if !found || p.RSSBytes > best.RSSBytes || (p.RSSBytes == best.RSSBytes && p.PID > best.PID) {
			best, found = p, true
		}
	}
	return best, found
}

// victim is the process to end for a tree over budget: the heaviest eligible
// one. Nothing when the tree is within budget, when the agent CLI alone holds
// the orçamento (ending its tools would not help), or when nothing is eligible.
func (t tree) victim(budget int64) (ports.ProcessInfo, bool) {
	if t.rss <= budget || t.agentRSS >= budget {
		return ports.ProcessInfo{}, false
	}
	return t.heaviest()
}

// command is how a process is named in an event: its name and at most its first
// argument, 80 characters at most. Never the full command line.
func command(p ports.ProcessInfo) string {
	name := filepath.Base(p.Name)
	if p.Arg != "" {
		name += " " + p.Arg
	}
	if len(name) > domain.MemoryCommandMax {
		name = name[:domain.MemoryCommandMax]
	}
	return name
}
