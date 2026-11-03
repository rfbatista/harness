package repoagents

import (
	"context"
	"sync"

	"operators-mcp/internal/domain"
)

// followerBuffer is how many changes a follower may lag behind before its
// channel is closed.
const followerBuffer = 64

// feed fans changes out to followers. A follower a whole buffer behind is
// dropped, its channel closed, so a slow client never holds up a write.
type feed struct {
	mu        sync.Mutex
	followers map[chan domain.RepositoryAgentsChanged]struct{}
}

func newFeed() *feed {
	return &feed{followers: map[chan domain.RepositoryAgentsChanged]struct{}{}}
}

func (f *feed) follow(ctx context.Context) <-chan domain.RepositoryAgentsChanged {
	ch := make(chan domain.RepositoryAgentsChanged, followerBuffer)
	f.mu.Lock()
	f.followers[ch] = struct{}{}
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.drop(ch)
	}()
	return ch
}

func (f *feed) announce(c domain.RepositoryAgentsChanged) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.followers {
		select {
		case ch <- c:
		default:
			delete(f.followers, ch)
			close(ch)
		}
	}
}

func (f *feed) drop(ch chan domain.RepositoryAgentsChanged) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.followers[ch]; ok {
		delete(f.followers, ch)
		close(ch)
	}
}
