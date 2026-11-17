package notifications

import (
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
)

// What each system kind says, in one place per kind (ADR 0095, UX study
// "Notifications" §3 and §8): the verb in the agent's voice, the object it
// acts on, and the plain-text title that reads as the actor, the verb and the
// object, so delivery providers say who and what too. Neither holds an id.

// what is the verb and object of a system notification.
type what struct {
	verb   string
	object string
	code   bool
	// colon joins the object to a verb that ends on a noun, as in "moved the
	// task to review: Notifications".
	colon bool
}

// apply sets the verb, the object and the title of n from w and its actor.
func (w what) apply(n *domain.Notification) {
	n.Verb = w.verb
	if w.object != "" {
		n.Object = &domain.NotificationObject{Text: w.object, Code: w.code}
	}
	n.Title = clip(w.title(n.Actor.Name), domain.MaxNotificationTitleLen)
}

func (w what) title(actor string) string {
	t := actor + " " + w.verb
	switch {
	case w.object == "":
	case w.colon:
		t += ": " + w.object
	default:
		t += " " + w.object
	}
	return t
}

func reviewWhat(subject string) what {
	return what{verb: "asks you to review", object: strings.TrimSpace(subject)}
}

// approvalWhat is a session waiting for the person's approval of lastAction,
// which the session keeps as "Tool: command" or just "Tool".
func approvalWhat(lastAction string) what {
	w := what{verb: "waits for your approval to run", object: "a tool", code: true}
	if la := strings.TrimSpace(lastAction); la != "" {
		w.object = commandOf(la)
	}
	return w
}

// commandOf is a session's last action, "Tool: command", as "command (Tool)".
func commandOf(lastAction string) string {
	if tool, command, ok := strings.Cut(lastAction, ": "); ok && tool != "" && command != "" {
		return command + " (" + tool + ")"
	}
	return lastAction
}

func interviewWhat(title string, questions int) what {
	verb := "has " + strconv.Itoa(questions) + " questions for you about"
	if questions == 1 {
		verb = "has 1 question for you about"
	}
	return what{verb: verb, object: strings.TrimSpace(title)}
}

func taskReviewWhat(taskTitle string) what {
	return what{verb: "moved the task to review", object: taskTitle, colon: true}
}

// failedWhat is a failed session; what it was running is the object when the
// session kept it.
func failedWhat(lastAction string) what {
	w := what{verb: "failed"}
	if la := strings.TrimSpace(lastAction); la != "" {
		w.verb, w.object, w.code = "failed while running", commandOf(la), true
	}
	return w
}

// commitCheckWhat is the harness failing to guard n sessions.
func commitCheckWhat(n int) what {
	verb := "could not guard " + strconv.Itoa(n) + " sessions"
	if n == 1 {
		verb = "could not guard 1 session"
	}
	return what{verb: verb, object: "commit checks off", colon: true}
}

// interruptedWhat is a restart that interrupted n sessions. The count is in
// the verb, so the title leaves the object out.
func interruptedWhat(n int) (w what, title string) {
	verb := "interrupted " + strconv.Itoa(n) + " sessions"
	if n == 1 {
		verb = "interrupted 1 session"
	}
	return what{verb: verb, object: "sessions"}, domain.ActorRestart + " " + verb
}

// branchRequestWhat is an agent's request for one branch action.
func branchRequestWhat(r domain.BranchRequest) what {
	branch := r.Branch
	switch {
	case r.Action == domain.BranchPush && r.Remote != nil:
		rb := r.Branch
		if r.RemoteBranch != nil {
			rb = *r.RemoteBranch
		}
		return what{verb: "wants to push", object: branch + " → " + r.Remote.Name + "/" + rb, code: true}
	case r.Action == domain.BranchPush:
		return what{verb: "wants to push", object: branch, code: true}
	case r.Action == domain.BranchRename && r.NewName != nil:
		return what{verb: "wants to rename", object: branch + " → " + *r.NewName, code: true}
	case r.Action == domain.BranchRemoteAdd && r.Remote != nil:
		return what{verb: "wants to add the remote", object: r.Remote.Name + " " + r.Remote.URL, code: true}
	case r.Action == domain.BranchRemoteSetURL && r.Remote != nil:
		return what{verb: "wants to change the URL of the remote", object: r.Remote.Name + " to " + r.Remote.URL, code: true}
	case r.Action == domain.BranchRemoteRename && r.Remote != nil && r.Remote.NewName != nil:
		return what{verb: "wants to rename the remote", object: r.Remote.Name + " → " + *r.Remote.NewName, code: true}
	case r.Action == domain.BranchRemoteRemove && r.Remote != nil:
		return what{verb: "wants to remove the remote", object: r.Remote.Name, code: true}
	case branch == "" && r.StartPoint != nil:
		return what{verb: "wants to " + string(r.Action) + " a branch at", object: r.StartPoint.Ref, code: true}
	}
	return what{verb: "wants to " + string(r.Action), object: branch, code: true}
}

// branchFindingWhat is the branch watch finding a change no request covers.
func branchFindingWhat(ev domain.BranchFindingFound) what {
	switch ev.Kind {
	case domain.FindingPush:
		return what{verb: "found a push with no approved request on", object: ev.Branch, code: true}
	case domain.FindingRemote:
		return what{verb: "found a remote with no approved request:", object: ev.Branch, code: true}
	case domain.FindingDelete:
		return what{verb: "found a branch deletion with no approved request on", object: ev.Branch, code: true}
	case domain.FindingRename:
		return what{verb: "found a rename with no approved request on", object: ev.Branch + " → " + ev.NewName, code: true}
	}
	return what{verb: "found a branch with no approved request:", object: ev.Branch, code: true}
}

// serviceRequestWhat is an agent's request to change a service; a start or
// restart names the ref it runs.
func serviceRequestWhat(r domain.ServiceRequest) what {
	object := r.ServiceName
	if ref := serviceRef(r); ref != "" {
		object += " on " + ref
	}
	return what{verb: "wants to " + string(r.Action), object: object, code: true}
}

// serviceRef is the branch or worktree a service request runs on, in words.
func serviceRef(r domain.ServiceRequest) string {
	switch {
	case r.Branch != nil && r.Branch.Name != "":
		return r.Branch.Name
	case r.Worktree == "main":
		return "main"
	case r.Worktree == "own":
		return "its own worktree"
	}
	return strings.TrimPrefix(r.Worktree, "branch:")
}
