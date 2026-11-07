// Package agentboard derives the Agent board: the seed of every task with a
// live session, and the live session feed that keeps it current (ADR 0068,
// "Contract: harness server ↔ web UI — the agent board seed and the live
// session feed").
package agentboard

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// followerBuffer is how many events a follower may fall behind before it is
// dropped and must read the seed again (ADR 0012).
const followerBuffer = 256

// waitingForMax is the longest waiting_for, the ellipsis included.
const waitingForMax = 120

// claudeName is the agent name of a session that has no agent.
const claudeName = "claude"

// Asks says what a session's pending approval asks for.
type Asks interface {
	// PendingAsk is the one-line ask of the session's oldest pending
	// approval; "" when it has none or the server cannot say.
	PendingAsk(sessionID string) string
}

// Deps are what the board reads. Every read is a plain store read: no git, no
// service that takes a lock a publisher may hold.
type Deps struct {
	Sessions ports.SessionRepository
	Tickets  ports.TicketRepository
	Projects ports.ProjectRepository
	Agents   ports.AgentRepository
	Asks     Asks // nil: waiting_for is always null
	// Catalog is followed for project renames; nil follows nothing.
	Catalog ports.ProjectCatalogFeed
	Clock   func() time.Time // nil: time.Now
}

// Service implements ports.AgentBoard. It is a ports.ProjectChangeObserver:
// every change put on a project's feed may change a card, and the events are
// found by recomputing the task's card and diffing it against the card last
// published.
type Service struct {
	Deps

	mu        sync.Mutex
	seq       int64
	primed    bool                         // cards and names hold what the stores held at some point
	cards     map[string]*domain.BoardCard // by task id: the last card published
	names     map[string]string            // project names, as last seen
	followers map[chan domain.BoardEvent]struct{}
}

var (
	_ ports.AgentBoard            = (*Service)(nil)
	_ ports.ProjectChangeObserver = (*Service)(nil)
)

// New returns a board reading deps.
func New(deps Deps) *Service {
	return &Service{Deps: deps, cards: map[string]*domain.BoardCard{}, names: map[string]string{},
		followers: map[chan domain.BoardEvent]struct{}{}}
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Seed reads the board in one pass: a fixed number of reads however many
// projects and tasks there are.
func (s *Service) Seed(ctx context.Context) (*domain.BoardSeed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cards, projects, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	s.cards = cards
	s.names = map[string]string{}
	for _, p := range projects {
		s.names[p.ID] = p.Name
	}
	s.primed = true

	byProject := map[string][]domain.BoardCard{}
	for _, c := range cards {
		byProject[c.ProjectID] = append(byProject[c.ProjectID], *c)
	}
	seed := &domain.BoardSeed{Seq: s.seq, GeneratedAt: s.now(), ProjectCount: len(projects), Projects: []domain.BoardProject{}}
	for _, p := range projects {
		cs := byProject[p.ID]
		if len(cs) == 0 {
			seed.QuietProjects++
			continue
		}
		sortCards(cs)
		seed.Projects = append(seed.Projects, domain.BoardProject{ProjectID: p.ID, ProjectName: p.Name, Cards: cs})
	}
	sort.SliceStable(seed.Projects, func(i, j int) bool {
		a, b := strings.ToLower(seed.Projects[i].ProjectName), strings.ToLower(seed.Projects[j].ProjectName)
		if a != b {
			return a < b
		}
		return seed.Projects[i].ProjectID < seed.Projects[j].ProjectID
	})
	return seed, nil
}

// Prime reads what is live now, so the first change after it is told against
// the cards that were already there. Seed does the same.
func (s *Service) Prime(ctx context.Context) error {
	_, err := s.Seed(ctx)
	return err
}

// load reads every live session with its task, feature, project and agent in
// a fixed number of queries and builds the cards.
func (s *Service) load(ctx context.Context) (map[string]*domain.BoardCard, []*domain.Project, error) {
	live, err := s.Sessions.Query(ctx, ports.SessionFilter{Statuses: domain.LiveSessionStatuses})
	if err != nil {
		return nil, nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, sess := range live {
		if sess.TicketID != "" && !seen[sess.TicketID] {
			seen[sess.TicketID] = true
			ids = append(ids, sess.TicketID)
		}
	}
	byTicket := map[string][]*domain.Session{}
	tickets := map[string]*domain.Ticket{}
	if len(ids) > 0 {
		// All of each task's sessions: a session's role depends on the
		// task's architect, which may have ended.
		all, err := s.Sessions.Query(ctx, ports.SessionFilter{TicketIDs: ids})
		if err != nil {
			return nil, nil, err
		}
		for _, sess := range all {
			byTicket[sess.TicketID] = append(byTicket[sess.TicketID], sess)
		}
		var parents []string
		for _, tk := range s.Tickets.ListByIDs(ctx, ids) {
			tickets[tk.ID] = tk
			if tk.ParentID != "" {
				parents = append(parents, tk.ParentID)
			}
		}
		for _, tk := range s.Tickets.ListByIDs(ctx, parents) {
			tickets[tk.ID] = tk
		}
	}
	projects := s.Projects.List(ctx)
	agents := s.agentNames(ctx)
	cards := map[string]*domain.BoardCard{}
	for _, id := range ids {
		tk := tickets[id]
		if tk == nil {
			continue
		}
		if card := s.buildCard(tk, tickets[tk.ParentID], byTicket[id], agents, s.cards[id]); card != nil {
			cards[id] = card
		}
	}
	return cards, projects, nil
}

func (s *Service) agentNames(ctx context.Context) map[string]string {
	names := map[string]string{}
	if s.Agents == nil {
		return names
	}
	for _, a := range s.Agents.List(ctx) {
		names[a.ID] = a.Name
	}
	return names
}

// buildCard is the card of task tk from all of its sessions: nil when none is
// live. prev is the card last published for it, which says when the card
// started needing the person.
func (s *Service) buildCard(tk, feature *domain.Ticket, sessions []*domain.Session, agents map[string]string, prev *domain.BoardCard) *domain.BoardCard {
	card := &domain.BoardCard{
		TaskID: tk.ID, ProjectID: tk.ProjectID, TaskTitle: tk.Title, TaskStatus: string(tk.Status),
		Sessions: []domain.BoardSessionLine{},
	}
	if tk.ParentID != "" && feature != nil {
		id, title := feature.ID, feature.Title
		card.FeatureID, card.FeatureTitle = &id, &title
	}
	var needSince *time.Time
	for _, sess := range sessions {
		if !sess.Status.IsLive() {
			continue
		}
		line := s.line(sess, sessions, agents)
		card.Sessions = append(card.Sessions, line)
		if line.StatusSince.After(card.LastStatusChangeAt) {
			card.LastStatusChangeAt = line.StatusSince
		}
		if sess.Status.NeedsPerson() {
			card.NeedsYou = true
			// Without a card to go by, the card needs the person since its
			// earliest still-waiting session began to.
			if needSince == nil || line.StatusSince.Before(*needSince) {
				t := line.StatusSince
				needSince = &t
			}
		}
	}
	if len(card.Sessions) == 0 {
		return nil
	}
	sort.SliceStable(card.Sessions, func(i, j int) bool {
		a, b := card.Sessions[i], card.Sessions[j]
		if !a.StatusSince.Equal(b.StatusSince) {
			return a.StatusSince.After(b.StatusSince)
		}
		return a.SessionID < b.SessionID
	})
	if card.NeedsYou {
		if prev != nil && prev.NeedsYou && prev.NeedsYouSince != nil {
			needSince = prev.NeedsYouSince // still needing: it did not start again
		}
		card.NeedsYouSince = needSince
	}
	return card
}

func (s *Service) line(sess *domain.Session, taskSessions []*domain.Session, agents map[string]string) domain.BoardSessionLine {
	name := agents[sess.AgentID]
	if name == "" {
		name = claudeName
	}
	role, _ := domain.RoleOf(sess.ID, taskSessions)
	roleName := string(role)
	if role == domain.RolePeer {
		roleName = "peer"
	}
	line := domain.BoardSessionLine{
		SessionID: sess.ID, AgentName: name, Role: roleName, Status: sess.Status,
		StatusSince: sess.StatusSince, Mode: string(sess.Mode), Model: sess.Model,
	}
	if sess.Status == domain.SessionWaitingApproval && s.Asks != nil {
		if ask := s.Asks.PendingAsk(sess.ID); ask != "" {
			ask = cut(ask)
			line.WaitingFor = &ask
		}
	}
	return line
}

// cut is ask on one line, at most waitingForMax characters, ending in "…" when
// it was cut.
func cut(ask string) string {
	ask = strings.Join(strings.Fields(ask), " ")
	r := []rune(ask)
	if len(r) <= waitingForMax {
		return ask
	}
	return string(r[:waitingForMax-1]) + "…"
}

// sortCards orders a project's cards: those that need the person first, oldest
// wait first; then the rest, newest status change first.
func sortCards(cards []domain.BoardCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if a.NeedsYou != b.NeedsYou {
			return a.NeedsYou
		}
		if a.NeedsYou && !a.NeedsYouSince.Equal(*b.NeedsYouSince) {
			return a.NeedsYouSince.Before(*b.NeedsYouSince)
		}
		if !a.NeedsYou && !a.LastStatusChangeAt.Equal(b.LastStatusChangeAt) {
			return a.LastStatusChangeAt.After(b.LastStatusChangeAt)
		}
		return a.TaskID < b.TaskID
	})
}

// Follow follows the live session feed until ctx ends. A follower that falls
// behind is dropped: its channel closes.
func (s *Service) Follow(ctx context.Context) (<-chan domain.BoardEvent, error) {
	c := make(chan domain.BoardEvent, followerBuffer)
	s.mu.Lock()
	s.followers[c] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.followers[c]; ok {
			delete(s.followers, c)
			close(c)
		}
	}()
	return c, nil
}

// publish numbers ev and hands it to every follower; one that cannot take it
// is dropped. Called with mu held.
func (s *Service) publish(ev domain.BoardEvent) {
	s.seq++
	ev.Seq = s.seq
	for c := range s.followers {
		select {
		case c <- ev:
		default:
			delete(s.followers, c)
			close(c)
		}
	}
}

// ProjectChanged derives the live session events a change on a project's feed
// causes.
func (s *Service) ProjectChanged(change ports.ProjectChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	switch {
	case change.Session != nil && change.Deleted:
		for tid, c := range s.cards {
			for _, l := range c.Sessions {
				if l.SessionID == change.Session.ID {
					s.recompute(ctx, tid)
				}
			}
		}
	case change.Session != nil:
		if change.Session.TicketID != "" {
			s.recompute(ctx, change.Session.TicketID)
		}
	case change.Ticket != nil:
		tid := change.Ticket.ID
		s.recompute(ctx, tid)
		// A feature's title (or its removal) is on its children's cards.
		for id, c := range s.cards {
			if id != tid && c.FeatureID != nil && *c.FeatureID == tid {
				s.recompute(ctx, id)
			}
		}
	case change.TaskStatus != nil:
		s.recompute(ctx, change.TaskStatus.TaskID)
	}
}

// recompute reads task tid's card as it is now, and publishes what differs
// from the card last published.
func (s *Service) recompute(ctx context.Context, tid string) {
	old := s.cards[tid]
	if old == nil && s.primed && !s.mayBeLive(ctx, tid) {
		return
	}
	sessions, err := s.Sessions.Query(ctx, ports.SessionFilter{TicketID: tid})
	if err != nil {
		return
	}
	tk := s.Tickets.Get(ctx, tid)
	var card *domain.BoardCard
	var projectID, projectName string
	if tk != nil {
		var feature *domain.Ticket
		if tk.ParentID != "" {
			feature = s.Tickets.Get(ctx, tk.ParentID)
		}
		card = s.buildCard(tk, feature, sessions, s.agentNames(ctx), old)
		projectID = tk.ProjectID
		if card != nil {
			if p := s.Projects.Get(ctx, tk.ProjectID); p != nil {
				projectName = p.Name
				if _, known := s.names[p.ID]; !known {
					s.names[p.ID] = p.Name // a rename is told by ProjectsChanged, not here
				}
			}
		}
	} else if old != nil {
		projectID = old.ProjectID
	}
	if sameCard(old, card) {
		return
	}
	if card == nil {
		delete(s.cards, tid)
	} else {
		s.cards[tid] = card
	}

	var oldLines map[string]domain.BoardSessionLine
	if old != nil {
		oldLines = map[string]domain.BoardSessionLine{}
		for _, l := range old.Sessions {
			oldLines[l.SessionID] = l
		}
	}
	emitted := false
	// Sessions that stopped being live.
	if old != nil {
		now := map[string]bool{}
		if card != nil {
			for _, l := range card.Sessions {
				now[l.SessionID] = true
			}
		}
		status := map[string]domain.SessionStatus{}
		for _, sess := range sessions {
			status[sess.ID] = sess.Status
		}
		for _, l := range old.Sessions {
			if now[l.SessionID] {
				continue
			}
			ended := "deleted"
			if st, ok := status[l.SessionID]; ok && st.IsTerminal() {
				ended = string(st)
			}
			emitted = true
			s.publish(domain.BoardEvent{Kind: domain.BoardSessionEnded, ProjectID: projectID, TaskID: tid,
				SessionID: l.SessionID, Ended: ended, Card: card})
		}
	}
	// Sessions that became live, or changed status, agent or role.
	if card != nil {
		for i := range card.Sessions {
			l := card.Sessions[i]
			if prev, ok := oldLines[l.SessionID]; ok && sameLine(prev, l) {
				continue
			}
			emitted = true
			s.publish(domain.BoardEvent{Kind: domain.BoardSessionLive, ProjectID: projectID, ProjectName: projectName,
				TaskID: tid, SessionID: l.SessionID, Session: &l, Card: card})
		}
	}
	// Otherwise the task itself changed.
	if card != nil && !emitted {
		s.publish(domain.BoardEvent{Kind: domain.BoardTaskChanged, ProjectID: projectID, ProjectName: projectName, TaskID: tid, Card: card})
	}
}

// mayBeLive says whether task tid has a live session; only asked when the
// card cache cannot tell.
func (s *Service) mayBeLive(ctx context.Context, tid string) bool {
	live, err := s.Sessions.Query(ctx, ports.SessionFilter{TicketID: tid, Statuses: domain.LiveSessionStatuses})
	return err != nil || len(live) > 0
}

func sameLine(a, b domain.BoardSessionLine) bool { return sameJSON(a, b) }

func sameCard(a, b *domain.BoardCard) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sameJSON(a, b)
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// ProjectsChanged announces a rename of a project that has a live session. It
// reads every project's name once, so a rename missed while the catalog feed
// was down is still found.
func (s *Service) ProjectsChanged(ctx context.Context) {
	projects := s.Projects.List(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	live := map[string]bool{}
	for _, c := range s.cards {
		live[c.ProjectID] = true
	}
	for _, p := range projects {
		if old, ok := s.names[p.ID]; ok && old != p.Name && live[p.ID] {
			s.publish(domain.BoardEvent{Kind: domain.BoardProjectChanged, ProjectID: p.ID, ProjectName: p.Name})
		}
		s.names[p.ID] = p.Name
	}
}

// Run follows the project catalog until ctx ends, announcing renames. It
// follows again when it fell behind.
func (s *Service) Run(ctx context.Context) {
	if s.Catalog == nil {
		return
	}
	for ctx.Err() == nil {
		changes, err := s.Catalog.FollowProjects(ctx)
		if err != nil {
			return
		}
		s.ProjectsChanged(ctx) // anything missed before the follow began
		for range changes {
			s.ProjectsChanged(ctx)
		}
	}
}
