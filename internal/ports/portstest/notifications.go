package portstest

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// NotificationsFixture is what the notifications suite needs besides the
// port: two projects, and a way to record notifications on the server side,
// since the person's port cannot create them.
type NotificationsFixture struct {
	// ProjectA and ProjectB are existing projects named "Alpha" and "Beta".
	ProjectA, ProjectB string
	// Post records an unread notification titled title in project, newer
	// than every one recorded before, and returns its id.
	Post func(t *testing.T, projectID, title string) string
}

// NotificationsConformance runs the inbox contract against a fresh, empty
// port from newPort.
func NotificationsConformance(t *testing.T, newPort func(t *testing.T) (ports.Notifications, NotificationsFixture)) {
	ctx := context.Background()

	ids := func(list []*domain.Notification) []string {
		out := make([]string, 0, len(list))
		for _, n := range list {
			out = append(out, n.ID)
		}
		return out
	}
	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("an empty inbox", func(t *testing.T) {
		p, _ := newPort(t)
		page, err := p.ListNotifications(ctx, ports.NotificationQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if page.Notifications == nil || len(page.Notifications) != 0 || page.UnreadCount != 0 || page.NextBefore != "" {
			t.Fatalf("empty inbox = %#v, want an empty list, no count, no cursor", page)
		}
	})

	t.Run("newest first, with names, across projects", func(t *testing.T) {
		p, f := newPort(t)
		a := f.Post(t, f.ProjectA, "first")
		b := f.Post(t, f.ProjectB, "second")
		page, err := p.ListNotifications(ctx, ports.NotificationQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(page.Notifications); !same(got, []string{b, a}) || page.UnreadCount != 2 {
			t.Fatalf("list = %v (unread %d), want [%s %s] and 2", got, page.UnreadCount, b, a)
		}
		n := page.Notifications[1]
		if n.ProjectID != f.ProjectA || n.ProjectName != "Alpha" || n.Title != "first" || n.Link == "" ||
			n.CreatedAt.IsZero() || n.ReadAt != nil || n.ResolvedAt != nil || !n.Unread() {
			t.Fatalf("notification = %+v", n)
		}
	})

	t.Run("a project filter keeps the global count", func(t *testing.T) {
		p, f := newPort(t)
		a := f.Post(t, f.ProjectA, "a")
		f.Post(t, f.ProjectB, "b")
		page, err := p.ListNotifications(ctx, ports.NotificationQuery{ProjectID: f.ProjectA})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(page.Notifications); !same(got, []string{a}) || page.UnreadCount != 2 {
			t.Fatalf("project list = %v (unread %d), want [%s] and the global 2", got, page.UnreadCount, a)
		}
	})

	t.Run("pages follow the cursor to the end", func(t *testing.T) {
		p, f := newPort(t)
		var want []string
		for _, title := range []string{"1", "2", "3", "4", "5"} {
			want = append([]string{f.Post(t, f.ProjectA, title)}, want...)
		}
		var got []string
		before := ""
		for pages := 0; ; pages++ {
			if pages > 3 {
				t.Fatalf("more than 3 pages of 2 for 5 notifications: %v", got)
			}
			page, err := p.ListNotifications(ctx, ports.NotificationQuery{Limit: 2, Before: before})
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, ids(page.Notifications)...)
			if page.NextBefore == "" {
				break
			}
			before = page.NextBefore
		}
		if !same(got, want) {
			t.Fatalf("paged = %v, want %v", got, want)
		}
	})

	t.Run("a bad limit or cursor is INVALID_INPUT", func(t *testing.T) {
		p, _ := newPort(t)
		for _, q := range []ports.NotificationQuery{{Limit: 201}, {Limit: -1}, {Before: "not a cursor"}} {
			_, err := p.ListNotifications(ctx, q)
			wantCode(t, err, "INVALID_INPUT")
		}
	})

	t.Run("marking ids read answers only what changed", func(t *testing.T) {
		p, f := newPort(t)
		a := f.Post(t, f.ProjectA, "a")
		b := f.Post(t, f.ProjectA, "b")
		c, err := p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a}})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(c.Changed); !same(got, []string{a}) || c.UnreadCount != 1 || c.Changed[0].ReadAt == nil {
			t.Fatalf("mark a = %+v, want a read and 1 unread", c)
		}
		c, err = p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a, b}})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(c.Changed); !same(got, []string{b}) || c.UnreadCount != 0 {
			t.Fatalf("mark a and b = %v (unread %d), want only b, 0", got, c.UnreadCount)
		}
		c, err = p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a}})
		if err != nil {
			t.Fatal(err)
		}
		if c.Changed == nil || len(c.Changed) != 0 {
			t.Fatalf("marking a read one again = %#v, want an empty change", c.Changed)
		}
		page, err := p.ListNotifications(ctx, ports.NotificationQuery{UnreadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Notifications) != 0 {
			t.Fatalf("unread after marking both = %v", ids(page.Notifications))
		}
	})

	t.Run("an unknown id marks nothing", func(t *testing.T) {
		p, f := newPort(t)
		a := f.Post(t, f.ProjectA, "a")
		_, err := p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a, "missing"}})
		wantCode(t, err, "NOTIFICATION_NOT_FOUND")
		page, err := p.ListNotifications(ctx, ports.NotificationQuery{UnreadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(page.Notifications); !same(got, []string{a}) || page.UnreadCount != 1 {
			t.Fatalf("after a failed mark = %v (unread %d), want a still unread", got, page.UnreadCount)
		}
	})

	t.Run("ids and all are exclusive", func(t *testing.T) {
		p, f := newPort(t)
		a := f.Post(t, f.ProjectA, "a")
		_, err := p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{})
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a}, All: true})
		wantCode(t, err, "INVALID_INPUT")
	})

	t.Run("all, narrowed to a project", func(t *testing.T) {
		p, f := newPort(t)
		a1 := f.Post(t, f.ProjectA, "a1")
		a2 := f.Post(t, f.ProjectA, "a2")
		b := f.Post(t, f.ProjectB, "b")
		c, err := p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{All: true, ProjectID: f.ProjectA})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(c.Changed); !same(got, []string{a2, a1}) || c.UnreadCount != 1 {
			t.Fatalf("all of Alpha = %v (unread %d), want [%s %s] and 1", got, c.UnreadCount, a2, a1)
		}
		c, err = p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{All: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(c.Changed); !same(got, []string{b}) || c.UnreadCount != 0 {
			t.Fatalf("all = %v (unread %d), want only %s, 0", got, c.UnreadCount, b)
		}
	})

	t.Run("followers get each change in commit order with its count", func(t *testing.T) {
		p, f := newPort(t)
		fctx, cancel := context.WithCancel(ctx)
		defer cancel()
		changes, err := p.FollowNotifications(fctx)
		if err != nil {
			t.Fatal(err)
		}
		a := f.Post(t, f.ProjectA, "a")
		b := f.Post(t, f.ProjectB, "b")
		if _, err := p.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{a}}); err != nil {
			t.Fatal(err)
		}
		want := []struct {
			id     string
			unread int
			read   bool
		}{{a, 1, false}, {b, 2, false}, {a, 1, true}}
		for i, w := range want {
			select {
			case c, ok := <-changes:
				if !ok {
					t.Fatalf("the stream closed before change %d", i)
				}
				if got := ids(c.Changed); !same(got, []string{w.id}) || c.UnreadCount != w.unread || (c.Changed[0].ReadAt != nil) != w.read {
					t.Fatalf("change %d = %v (unread %d, read %v), want [%s], %d, read %v",
						i, got, c.UnreadCount, c.Changed[0].ReadAt != nil, w.id, w.unread, w.read)
				}
				if c.Changed[0].ProjectName == "" {
					t.Fatalf("change %d carries no project name: %+v", i, c.Changed[0])
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no change %d", i)
			}
		}
	})

	t.Run("ending the follow closes the stream", func(t *testing.T) {
		p, _ := newPort(t)
		fctx, cancel := context.WithCancel(ctx)
		changes, err := p.FollowNotifications(fctx)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case _, ok := <-changes:
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("the stream stayed open after the follow ended")
			}
		}
	})
}
