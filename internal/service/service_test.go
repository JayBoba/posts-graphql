package service_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/domain"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/memory"
)

type fakeBroker struct {
	mu        sync.Mutex
	published []domain.Comment
}

func (b *fakeBroker) Publish(c domain.Comment) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, c)
}

func (b *fakeBroker) Subscribe(context.Context, int64) <-chan *domain.Comment { return nil }

func newService(t *testing.T) (*service.Service, *fakeBroker) {
	t.Helper()
	b := &fakeBroker{}
	return service.New(memory.New(), b), b
}

func createPost(t *testing.T, svc *service.Service, author string, enabled bool) domain.Post {
	t.Helper()
	p, err := svc.CreatePost(context.Background(), domain.Post{
		AuthorID: author, Title: "title", Body: "body", CommentsEnabled: enabled,
	})
	require.NoError(t, err)
	return p
}

func ptr[T any](v T) *T { return &v }

func TestCreatePost_Validation(t *testing.T) {
	svc, _ := newService(t)

	tests := []struct {
		name    string
		author  string
		title   string
		body    string
		wantErr error
	}{
		{"ok", "alice", "title", "body", nil},
		{"empty author", " ", "title", "body", domain.ErrInvalidInput},
		{"100 chars in author is ok", strings.Repeat("я", 100), "title", "body", nil},
		{"101 chars in author is too long", strings.Repeat("я", 101), "title", "body", domain.ErrInvalidInput},
		{"empty title", "alice", " \t", "body", domain.ErrInvalidInput},
		{"empty body", "alice", "title", "\n", domain.ErrInvalidInput},
		{"300 cyrillic chars in title is ok", "alice", strings.Repeat("я", 300), "body", nil},
		{"301 chars in title is too long", "alice", strings.Repeat("я", 301), "body", domain.ErrInvalidInput},
		{"100000 chars in body is ok", "alice", "title", strings.Repeat("я", 100_000), nil},
		{"100001 chars in body is too long", "alice", "title", strings.Repeat("я", 100_001), domain.ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreatePost(context.Background(), domain.Post{
				AuthorID: tt.author, Title: tt.title, Body: tt.body,
			})
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestCreateComment_Validation(t *testing.T) {
	svc, _ := newService(t)
	post := createPost(t, svc, "author", true)

	tests := []struct {
		name    string
		author  string
		body    string
		wantErr error
	}{
		{"ok", "bob", "hello", nil},
		{"empty author", "  ", "hello", domain.ErrInvalidInput},
		{"101 chars in author is too long", strings.Repeat("я", 101), "hello", domain.ErrInvalidInput},
		{"empty body", "bob", "", domain.ErrInvalidInput},
		{"only whitespace", "bob", "  \n\t", domain.ErrInvalidInput},
		{"2000 cyrillic chars is ok", "bob", strings.Repeat("я", 2000), nil},
		{"2001 chars is too long", "bob", strings.Repeat("я", 2001), domain.ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateComment(context.Background(), domain.Comment{
				PostID: post.ID, AuthorID: tt.author, Body: tt.body,
			})
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestCreateComment_Rules(t *testing.T) {
	ctx := context.Background()
	svc, broker := newService(t)
	open := createPost(t, svc, "author", true)
	closed := createPost(t, svc, "author", false)
	other := createPost(t, svc, "author", true)

	root, err := svc.CreateComment(ctx, domain.Comment{PostID: open.ID, AuthorID: "bob", Body: "root"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		postID   int64
		parentID *int64
		wantErr  error
	}{
		{"reply to existing comment", open.ID, ptr(root.ID), nil},
		{"post not found", 999, nil, domain.ErrPostNotFound},
		{"comments disabled", closed.ID, nil, domain.ErrCommentsDisabled},
		{"parent not found", open.ID, ptr(int64(999)), domain.ErrParentNotFound},
		{"parent from another post", other.ID, ptr(root.ID), domain.ErrParentNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateComment(ctx, domain.Comment{
				PostID: tt.postID, ParentID: tt.parentID, AuthorID: "bob", Body: "text",
			})
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
	require.Len(t, broker.published, 2)
}

func TestSetCommentsEnabled(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	post := createPost(t, svc, "author", true)

	_, err := svc.SetCommentsEnabled(ctx, post.ID, "stranger", false)
	require.ErrorIs(t, err, domain.ErrForbidden)

	updated, err := svc.SetCommentsEnabled(ctx, post.ID, "author", false)
	require.NoError(t, err)
	require.False(t, updated.CommentsEnabled)

	_, err = svc.CreateComment(ctx, domain.Comment{PostID: post.ID, AuthorID: "bob", Body: "hi"})
	require.ErrorIs(t, err, domain.ErrCommentsDisabled)

	// The author is matched after trimming, just like on creation.
	updated, err = svc.SetCommentsEnabled(ctx, post.ID, "  author ", true)
	require.NoError(t, err)
	require.True(t, updated.CommentsEnabled)

	_, err = svc.SetCommentsEnabled(ctx, 999, "author", true)
	require.ErrorIs(t, err, domain.ErrPostNotFound)
}

func TestListComments_Pagination(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	post := createPost(t, svc, "author", true)

	var rootIDs []int64
	for range 5 {
		c, err := svc.CreateComment(ctx, domain.Comment{PostID: post.ID, AuthorID: "bob", Body: "root"})
		require.NoError(t, err)
		rootIDs = append(rootIDs, c.ID)
	}
	_, err := svc.CreateComment(ctx, domain.Comment{
		PostID: post.ID, ParentID: ptr(rootIDs[0]), AuthorID: "bob", Body: "reply",
	})
	require.NoError(t, err)
	var got []int64
	var after *int64
	for {
		page, err := svc.ListComments(ctx, post.ID, nil, after, 2)
		require.NoError(t, err)
		for _, c := range page.Items {
			got = append(got, c.ID)
		}
		if !page.HasNext {
			break
		}
		after = ptr(page.Items[len(page.Items)-1].ID)
	}
	require.Equal(t, rootIDs, got)

	_, err = svc.ListComments(ctx, post.ID, nil, nil, 0)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}
func TestCreateComment_Concurrent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	post := createPost(t, svc, "author", true)

	const n = 100
	ids := make(chan int64, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := svc.CreateComment(ctx, domain.Comment{PostID: post.ID, AuthorID: "bob", Body: "hi"})
			if err == nil {
				ids <- c.ID
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[int64]bool)
	for id := range ids {
		require.False(t, seen[id], "duplicate id %d", id)
		seen[id] = true
	}
	require.Len(t, seen, n)
}

func TestListRepliesBatch(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	post := createPost(t, svc, "author", true)

	newComment := func(parentID *int64) domain.Comment {
		c, err := svc.CreateComment(ctx, domain.Comment{PostID: post.ID, ParentID: parentID, AuthorID: "bob", Body: "text"})
		require.NoError(t, err)
		return c
	}
	a := newComment(nil)
	b := newComment(nil)
	for range 3 {
		newComment(ptr(a.ID))
	}

	pages, err := svc.ListRepliesBatch(ctx, []int64{a.ID, b.ID}, 2)
	require.NoError(t, err)
	require.Len(t, pages[a.ID].Items, 2)
	require.True(t, pages[a.ID].HasNext)
	require.Empty(t, pages[b.ID].Items)
	require.False(t, pages[b.ID].HasNext)
}
