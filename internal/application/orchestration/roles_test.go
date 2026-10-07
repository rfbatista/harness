package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeRoles answers the server's role lookup from a map; err fails every call.
type fakeRoles struct {
	roles map[string]domain.SessionRole
	err   error
}

func (f *fakeRoles) SessionRole(_ context.Context, id string) (domain.SessionRole, error) {
	return f.roles[id], f.err
}

func briefOf(t *testing.T, l launched) string {
	t.Helper()
	v, ok := flag(l.Args, "--append-system-prompt")
	if !ok {
		t.Fatalf("no --append-system-prompt: %q", l.Args)
	}
	return v
}

const (
	delegateMark  = "mcp__task__message_architect"
	architectMark = "mcp__task__reply_to_session"
	peerMark      = "Move the task as the work moves"
)

// A new session is briefed for the role it will have: the parent's role
// decides it before the session is stored.
func TestStartInteractive_BriefsTheNewSessionForItsRole(t *testing.T) {
	roles := &fakeRoles{roles: map[string]domain.SessionRole{"arch": domain.RoleArchitect, "dlg": domain.RoleDelegate}}
	cases := []struct {
		name string
		req  InteractiveRequest
		want string
	}{
		{"started by the architect", InteractiveRequest{ParentSessionID: "arch"}, delegateMark},
		{"started by a delegate", InteractiveRequest{ParentSessionID: "dlg"}, delegateMark},
		{"started by a peer", InteractiveRequest{ParentSessionID: "someone"}, peerMark},
		{"started by a person", InteractiveRequest{}, peerMark},
		{"started in architect mode", InteractiveRequest{Mode: "architect"}, architectMark},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			svc.Roles = roles
			_, launch := startInteractive(t, svc, c.req)
			brief := briefOf(t, launch)
			if !strings.Contains(brief, c.want) {
				t.Fatalf("brief lacks %q:\n%s", c.want, brief)
			}
			for _, other := range []string{delegateMark, architectMark, peerMark} {
				if other != c.want && strings.Contains(brief, other) {
					t.Fatalf("brief also carries %q:\n%s", other, brief)
				}
			}
		})
	}
}

// Without a lookup the server has no architect channel; a failed lookup must
// not fail the start. Both brief the session as a peer.
func TestStartInteractive_BriefsAPeerWithoutARoleLookup(t *testing.T) {
	for name, roles := range map[string]*fakeRoles{
		"no lookup":     nil,
		"lookup failed": {err: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			if roles != nil {
				svc.Roles = roles
			}
			_, launch := startInteractive(t, svc, InteractiveRequest{ParentSessionID: "arch", Mode: "architect"})
			if name == "no lookup" {
				if brief := briefOf(t, launch); !strings.Contains(brief, peerMark) {
					t.Fatalf("brief is not a peer's:\n%s", brief)
				}
				return
			}
			// Architect mode needs no lookup; a delegate's start does.
			_, launch = startInteractive(t, svc, InteractiveRequest{ParentSessionID: "arch"})
			if brief := briefOf(t, launch); !strings.Contains(brief, peerMark) {
				t.Fatalf("brief is not a peer's:\n%s", brief)
			}
		})
	}
}

// A resumed session is briefed for the role it has now.
func TestResumeInteractive_BriefsTheSessionForItsCurrentRole(t *testing.T) {
	svc, _ := newInteractiveService(t)
	roles := &fakeRoles{roles: map[string]domain.SessionRole{}}
	svc.Roles = roles
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	roles.roles[sess.ID] = domain.RoleDelegate
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if brief := briefOf(t, launchOf(t, spec)); !strings.Contains(brief, delegateMark) {
		t.Fatalf("resumed brief is not a delegate's:\n%s", brief)
	}
}
