package pubsub

import (
	"context"
	"sync"

	"github.com/JayBoba/posts-graphql/internal/domain"
)

const subscriberBuffer = 16

type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan *domain.Comment]struct{}
}

func New() *Hub {
	return &Hub{subs: make(map[int64]map[chan *domain.Comment]struct{})}
}

func (h *Hub) Subscribe(ctx context.Context, postID int64) <-chan *domain.Comment {
	ch := make(chan *domain.Comment, subscriberBuffer)

	h.mu.Lock()
	if h.subs[postID] == nil {
		h.subs[postID] = make(map[chan *domain.Comment]struct{})
	}
	h.subs[postID][ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subs[postID], ch)
		if len(h.subs[postID]) == 0 {
			delete(h.subs, postID)
		}
		close(ch)
		h.mu.Unlock()
	}()

	return ch
}

func (h *Hub) Publish(c domain.Comment) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subs[c.PostID] {
		select {
		case ch <- &c:
		default:
		}
	}
}
