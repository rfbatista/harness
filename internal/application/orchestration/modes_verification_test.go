package orchestration

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What each verification mode (ADR 0090) is opened with: its skill line, the
// closing that tells it what to take in, what to write and how to report.
var verificationModes = []struct {
	mode     string
	skills   []string
	model    string
	closing  []string
	document string
}{
	{"review", []string{"task-review", "task-code-review"}, "opus",
		[]string{"git diff <base>...HEAD", "Review: <branch>", "approve", "request changes", "Write no code"}, "Review: <branch>"},
	{"test", []string{"task-test", "task-tdd"}, "sonnet",
		[]string{"diff of your branch", "task-tdd", "Test report: <branch>", "stays failing", "Change no production code"}, "Test report: <branch>"},
	{"qa", []string{"task-qa"}, "sonnet",
		[]string{"browser tools", "list_services", "task/designs/", "QA: <task title>", "Write no code"}, "QA: <task title>"},
	{"operate", []string{"task-operate", "task-run-app"}, "sonnet",
		[]string{"service request", "worktree: \"branch:<name>\"", "run overrides", "task-run-app", "Write no code"}, ""},
}

func TestStartInteractive_VerificationModes(t *testing.T) {
	for _, vm := range verificationModes {
		t.Run(vm.mode, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			svc.Settings = modelSettings{}
			sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: vm.mode, Prompt: "Check agent/feature-x."})

			if string(sess.Mode) != vm.mode {
				t.Fatalf("mode = %q, want %q", sess.Mode, vm.mode)
			}
			for _, name := range vm.skills {
				if body := pluginSkill(t, launch, name); !strings.Contains(body, "name: "+name) {
					t.Errorf("%s SKILL.md = %q", name, body)
				}
			}
			p := launch.Spec.Prompt
			if want := "Use the " + vm.skills[0] + " skill on task tk1: Ship the thing"; !strings.HasPrefix(p, want) {
				t.Errorf("first message does not start with %q: %q", want, p)
			}
			for _, w := range vm.closing {
				if !strings.Contains(p, w) {
					t.Errorf("first message lacks %q: %q", w, p)
				}
			}
			if !strings.HasSuffix(p, "Check agent/feature-x.") {
				t.Errorf("first message does not end with what the starter typed: %q", p)
			}
			if !strings.Contains(p, "Never change the task status") {
				t.Errorf("first message lacks the status rule: %q", p)
			}
			if got := modelOf(launch); got != vm.model {
				t.Errorf("--model = %q, want %q", got, vm.model)
			}
			if got := sess.Sandbox; got != nil && got.Visibility != domain.SandboxOwnRepo {
				t.Errorf("visibility = %q, want own repo", got.Visibility)
			}
			blocked := svc.branchGateInstructionFile(domain.SessionMode(vm.mode))
			if !strings.Contains(string(blocked.Content), "BRANCH_ACTION_BLOCKED") {
				t.Errorf("the branch block is not on for %s", vm.mode)
			}
		})
	}
}

func TestStartInteractive_VerificationModeModelFollowsItsSetting(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Settings = modelSettings{domain.ModeModelKey(domain.SessionModeReview): "haiku", domain.ModeModelKey(domain.SessionModeQA): domain.ModelDefault}
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "review"})
	if got := modelOf(launch); got != "haiku" {
		t.Errorf("review --model = %q, want haiku", got)
	}
	_, launch = startInteractive(t, svc, InteractiveRequest{Mode: "qa"})
	if got := modelOf(launch); got != "" {
		t.Errorf("qa --model = %q, want none", got)
	}
	_, launch = startInteractive(t, svc, InteractiveRequest{Mode: "test", Model: "fable"})
	if got := modelOf(launch); got != "fable" {
		t.Errorf("test --model = %q, want the override", got)
	}
}

// A delegate reports to its architect; a peer, which has no architect, sums
// up in a task comment instead. The role decides, not the parent's presence.
func TestStartInteractive_VerificationOpeningFollowsTheRole(t *testing.T) {
	roles := &fakeRoles{roles: map[string]domain.SessionRole{"arch": domain.RoleArchitect, "peer": domain.RolePeer}}
	cases := []struct {
		name   string
		parent string
		report bool
	}{
		{"started by the architect", "arch", true},
		{"started by a peer", "peer", false},
		{"started by a person", "", false},
	}
	for _, vm := range verificationModes {
		for _, c := range cases {
			t.Run(vm.mode+" "+c.name, func(t *testing.T) {
				svc, _ := newInteractiveService(t)
				svc.Roles = roles
				_, launch := startInteractive(t, svc, InteractiveRequest{Mode: vm.mode, ParentSessionID: c.parent})
				p := launch.Spec.Prompt
				if got := strings.Contains(p, "message_architect"); got != c.report {
					t.Errorf("names message_architect = %v, want %v: %q", got, c.report, p)
				}
				if got := strings.Contains(p, "sum up in a task comment"); got == c.report {
					t.Errorf("names the task comment = %v, want %v: %q", got, !c.report, p)
				}
			})
		}
	}
}

func TestResumeInteractive_KeepsTheVerificationSkills(t *testing.T) {
	for _, vm := range verificationModes {
		t.Run(vm.mode, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: vm.mode})
			if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
				t.Fatal(err)
			}
			_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range vm.skills {
				pluginSkill(t, launchOf(t, spec), name)
			}
		})
	}
}

func TestStartInteractive_VerificationModesNeedATaskAndRefuseADesignSystem(t *testing.T) {
	for _, vm := range verificationModes {
		svc, _ := newInteractiveService(t)
		_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: vm.mode})
		if codeOf(err) != "INVALID_INPUT" {
			t.Errorf("%s without a task: %v, want INVALID_INPUT", vm.mode, err)
		}
		_, _, err = svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: vm.mode, DesignSystemID: "ds1"})
		if codeOf(err) != "INVALID_INPUT" {
			t.Errorf("%s with a design system: %v, want INVALID_INPUT", vm.mode, err)
		}
	}
}
