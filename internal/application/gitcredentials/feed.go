package gitcredentials

import (
	"context"
	"sync"

	"operators-mcp/internal/ports"
)

// followerBuffer is how many changes a follower may lag behind before its
// channel is closed.
const followerBuffer = 64

// feed fans credential changes out to followers. A follower that falls a
// whole buffer behind is dropped, its channel closed, so a slow client never
// holds up a write.
type feed struct {
	mu        *sync.Mutex
	followers map[chan ports.GitCredentialChange]struct{}
}

func newFeed() feed {
	return feed{mu: &sync.Mutex{}, followers: map[chan ports.GitCredentialChange]struct{}{}}
}

func (f feed) follow(ctx context.Context) <-chan ports.GitCredentialChange {
	ch := make(chan ports.GitCredentialChange, followerBuffer)
	f.mu.Lock()
	f.followers[ch] = struct{}{}
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.drop(ch)
	}()
	return ch
}

func (f feed) announce(c ports.GitCredentialChange) {
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

// drop closes ch unless announce already did.
func (f feed) drop(ch chan ports.GitCredentialChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.followers[ch]; ok {
		delete(f.followers, ch)
		close(ch)
	}
}
