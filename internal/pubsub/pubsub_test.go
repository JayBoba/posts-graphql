package pubsub_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/domain"
	"github.com/JayBoba/posts-graphql/internal/pubsub"
	"github.com/JayBoba/posts-graphql/internal/service"
)

// Hub implements the service's Broker interface. The check lives in the test,
// so the pubsub package itself does not depend on the service.
var _ service.Broker = (*pubsub.Hub)(nil)

func TestHub(t *testing.T) {
	hub := pubsub.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := hub.Subscribe(ctx, 1)
	otherPost := hub.Subscribe(ctx, 2)

	hub.Publish(domain.Comment{ID: 10, PostID: 1})

	select {
	case c := <-ch:
		require.Equal(t, int64(10), c.ID)
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the comment")
	}

	select {
	case <-otherPost:
		t.Fatal("subscriber of another post received the comment")
	default:
	}

	cancel()
	select {
	case _, ok := <-ch:
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("channel was not closed after cancel")
	}
}
