package services

import (
	"context"
	"sync"

	"operators-mcp/internal/ports"
)

// followerBuffer is how many events a follower may lag behind before its
// channel is closed.
const followerBuffer = 64

// feed fans each project's service events out to its followers. A follower
// that falls a whole buffer behind is dropped, its channel closed, so a slow
// client never holds up a change.
type feed struct {
	mu        sync.Mutex
	followers map[string]map[chan ports.ServiceEvent]struct{}
}

func newFeed() *feed {
	return &feed{followers: map[string]map[chan ports.ServiceEvent]struct{}{}}
}

// follow registers a follower of the project's events until ctx ends.
func (f *feed) follow(ctx context.Context, projectID string) <-chan ports.ServiceEvent {
	ch := make(chan ports.ServiceEvent, followerBuffer)
	f.mu.Lock()
	if f.followers[projectID] == nil {
		f.followers[projectID] = map[chan ports.ServiceEvent]struct{}{}
	}
	f.followers[projectID][ch] = struct{}{}
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.drop(projectID, ch)
	}()
	return ch
}

func (f *feed) announce(projectID string, e ports.ServiceEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.followers[projectID] {
		select {
		case ch <- e:
		default:
			delete(f.followers[projectID], ch)
			close(ch)
		}
	}
}

// drop closes ch unless announce already did.
func (f *feed) drop(projectID string, ch chan ports.ServiceEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.followers[projectID][ch]; ok {
		delete(f.followers[projectID], ch)
		close(ch)
	}
}
