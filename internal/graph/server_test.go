package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/99designs/gqlgen/client"
	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/graph"
	"github.com/JayBoba/posts-graphql/internal/pubsub"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/memory"
)

// spyStorage counts storage calls and can simulate a storage failure.
type spyStorage struct {
	*memory.Storage
	listComments     atomic.Int32
	listRepliesBatch atomic.Int32
	failListPosts    error
}

func (s *spyStorage) ListComments(ctx context.Context, postID int64, parentID, after *int64, limit int) ([]service.Comment, error) {
	s.listComments.Add(1)
	return s.Storage.ListComments(ctx, postID, parentID, after, limit)
}

func (s *spyStorage) ListRepliesBatch(ctx context.Context, parentIDs []int64, limit int) (map[int64][]service.Comment, error) {
	s.listRepliesBatch.Add(1)
	return s.Storage.ListRepliesBatch(ctx, parentIDs, limit)
}

func (s *spyStorage) ListPosts(ctx context.Context, after *int64, limit int) ([]service.Post, error) {
	if s.failListPosts != nil {
		return nil, s.failListPosts
	}
	return s.Storage.ListPosts(ctx, after, limit)
}

func newClient(t *testing.T) (*client.Client, *service.Service, *spyStorage) {
	t.Helper()
	store := &spyStorage{Storage: memory.New()}
	svc := service.New(store, pubsub.New())
	return client.New(graph.NewServer(svc)), svc, store
}

type gqlError struct {
	Message    string
	Extensions map[string]any
}

func post(t *testing.T, c *client.Client, query string, opts ...client.Option) (any, []gqlError) {
	t.Helper()
	resp, err := c.RawPost(query, opts...)
	require.NoError(t, err)
	var errs []gqlError
	if len(resp.Errors) > 0 {
		require.NoError(t, json.Unmarshal(resp.Errors, &errs))
	}
	return resp.Data, errs
}

func TestErrorCodes(t *testing.T) {
	c, svc, store := newClient(t)
	ctx := context.Background()
	p, err := svc.CreatePost(ctx, service.Post{AuthorID: "author", Title: "t", Body: "b", CommentsEnabled: true})
	require.NoError(t, err)

	tests := []struct {
		name     string
		query    string
		vars     map[string]any
		wantCode string
	}{
		{
			name:     "empty comment",
			query:    `mutation($p: ID!) { createComment(input: {postId: $p, authorId: "bob", body: " "}) { id } }`,
			vars:     map[string]any{"p": p.ID},
			wantCode: "BAD_USER_INPUT",
		},
		{
			name:     "post not found",
			query:    `mutation { createComment(input: {postId: 999, authorId: "bob", body: "hi"}) { id } }`,
			wantCode: "NOT_FOUND",
		},
		{
			name:     "not the author",
			query:    `mutation($p: ID!) { setCommentsEnabled(postId: $p, authorId: "stranger", enabled: false) { id } }`,
			vars:     map[string]any{"p": p.ID},
			wantCode: "FORBIDDEN",
		},
		{
			name:     "broken cursor",
			query:    `{ posts(after: "not-a-cursor") { edges { cursor } } }`,
			wantCode: "BAD_USER_INPUT",
		},
		{
			name:     "malformed id",
			query:    `{ post(id: "abc") { id } }`,
			wantCode: "BAD_USER_INPUT",
		},
		{
			name:     "non-positive id",
			query:    `{ comment(id: 0) { id } }`,
			wantCode: "BAD_USER_INPUT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []client.Option
			for k, v := range tt.vars {
				opts = append(opts, client.Var(k, v))
			}
			_, errs := post(t, c, tt.query, opts...)
			require.Len(t, errs, 1)
			require.Equal(t, tt.wantCode, errs[0].Extensions["code"])
		})
	}

	t.Run("storage failure is masked", func(t *testing.T) {
		store.failListPosts = errors.New(`pq: relation "posts" does not exist`)
		defer func() { store.failListPosts = nil }()

		_, errs := post(t, c, `{ posts { edges { cursor } } }`)
		require.Len(t, errs, 1)
		require.Equal(t, "INTERNAL", errs[0].Extensions["code"])
		require.Equal(t, "internal error", errs[0].Message)
	})

	t.Run("missing post is null, not an error", func(t *testing.T) {
		data, errs := post(t, c, `{ post(id: 999) { id } }`)
		require.Empty(t, errs)
		require.Equal(t, map[string]any{"post": nil}, data)
	})
}

func TestReplies_NoNPlusOne(t *testing.T) {
	c, svc, store := newClient(t)
	ctx := context.Background()
	p, err := svc.CreatePost(ctx, service.Post{AuthorID: "author", Title: "t", Body: "b", CommentsEnabled: true})
	require.NoError(t, err)

	comment := func(parentID *int64) int64 {
		cm, err := svc.CreateComment(ctx, service.Comment{PostID: p.ID, ParentID: parentID, AuthorID: "bob", Body: "text"})
		require.NoError(t, err)
		return cm.ID
	}
	// 5 top-level comments, each with 2 replies, each reply with one more reply.
	for range 5 {
		root := comment(nil)
		for range 2 {
			reply := comment(&root)
			comment(&reply)
		}
	}

	var resp struct {
		Post struct {
			Comments struct {
				Edges []struct {
					Node struct {
						Replies struct {
							Edges []struct {
								Node struct {
									Replies struct {
										Edges []struct{ Node struct{ ID int64 } }
									}
								}
							}
						}
					}
				}
			}
		}
	}
	c.MustPost(`query($id: ID!) {
		post(id: $id) { comments { edges { node {
			replies { edges { node {
				replies { edges { node { id } } }
			} } }
		} } } }
	}`, &resp, client.Var("id", p.ID))

	roots := resp.Post.Comments.Edges
	require.Len(t, roots, 5)
	for _, r := range roots {
		require.Len(t, r.Node.Replies.Edges, 2)
		for _, rep := range r.Node.Replies.Edges {
			require.Len(t, rep.Node.Replies.Edges, 1)
		}
	}
	// One call for top-level comments and one batch per nesting level,
	// instead of 1 + 5 + 10 separate calls.
	require.EqualValues(t, 1, store.listComments.Load())
	require.EqualValues(t, 2, store.listRepliesBatch.Load())
}

func TestComments_CursorPagination(t *testing.T) {
	c, svc, _ := newClient(t)
	ctx := context.Background()
	p, err := svc.CreatePost(ctx, service.Post{AuthorID: "author", Title: "t", Body: "b", CommentsEnabled: true})
	require.NoError(t, err)

	var want []int64
	for range 5 {
		cm, err := svc.CreateComment(ctx, service.Comment{PostID: p.ID, AuthorID: "bob", Body: "text"})
		require.NoError(t, err)
		want = append(want, cm.ID)
	}

	var got []int64
	var after *string
	for {
		var resp struct {
			Post struct {
				Comments struct {
					Edges    []struct{ Node struct{ ID int64 } }
					PageInfo struct {
						EndCursor   *string
						HasNextPage bool
					}
				}
			}
		}
		c.MustPost(`query($id: ID!, $after: String) {
			post(id: $id) { comments(first: 2, after: $after) {
				edges { node { id } }
				pageInfo { endCursor hasNextPage }
			} }
		}`, &resp, client.Var("id", p.ID), client.Var("after", after))

		for _, e := range resp.Post.Comments.Edges {
			got = append(got, e.Node.ID)
		}
		if !resp.Post.Comments.PageInfo.HasNextPage {
			break
		}
		after = resp.Post.Comments.PageInfo.EndCursor
	}
	require.Equal(t, want, got)
}

func TestPostsAndThreads(t *testing.T) {
	c, svc, _ := newClient(t)
	ctx := context.Background()

	var created struct {
		CreatePost struct {
			ID              int64
			AuthorID        string
			CommentsEnabled bool
		}
	}
	c.MustPost(`mutation { createPost(input: {authorId: " alice ", title: "first", body: "b"}) { id authorId commentsEnabled } }`, &created)
	first := created.CreatePost
	require.Equal(t, "alice", first.AuthorID)
	require.True(t, first.CommentsEnabled)

	c.MustPost(`mutation { createPost(input: {authorId: "alice", title: "second", body: "b", commentsEnabled: false}) { id authorId commentsEnabled } }`, &created)
	second := created.CreatePost
	require.False(t, second.CommentsEnabled)

	type postsPage struct {
		Posts struct {
			Edges    []struct{ Node struct{ ID int64 } }
			PageInfo struct {
				EndCursor   string
				HasNextPage bool
			}
		}
	}
	// Newest first.
	var page postsPage
	c.MustPost(`{ posts(first: 1) { edges { node { id } } pageInfo { endCursor hasNextPage } } }`, &page)
	require.Len(t, page.Posts.Edges, 1)
	require.Equal(t, second.ID, page.Posts.Edges[0].Node.ID)
	require.True(t, page.Posts.PageInfo.HasNextPage)

	c.MustPost(`query($after: String) { posts(first: 1, after: $after) { edges { node { id } } pageInfo { endCursor hasNextPage } } }`,
		&page, client.Var("after", page.Posts.PageInfo.EndCursor))
	require.Len(t, page.Posts.Edges, 1)
	require.Equal(t, first.ID, page.Posts.Edges[0].Node.ID)
	require.False(t, page.Posts.PageInfo.HasNextPage)

	// A deep thread is continued from any comment via comment(id), and its
	// replies are paginated with a cursor too.
	root, err := svc.CreateComment(ctx, service.Comment{PostID: first.ID, AuthorID: "bob", Body: "root"})
	require.NoError(t, err)
	var replies []int64
	for range 3 {
		r, err := svc.CreateComment(ctx, service.Comment{PostID: first.ID, ParentID: &root.ID, AuthorID: "bob", Body: "reply"})
		require.NoError(t, err)
		replies = append(replies, r.ID)
	}

	type thread struct {
		Comment *struct {
			ID      int64
			Replies struct {
				Edges    []struct{ Node struct{ ID int64 } }
				PageInfo struct {
					EndCursor   string
					HasNextPage bool
				}
			}
		}
	}
	const threadQuery = `query($id: ID!, $after: String) {
		comment(id: $id) { id replies(first: 2, after: $after) {
			edges { node { id } }
			pageInfo { endCursor hasNextPage }
		} }
	}`
	var th thread
	c.MustPost(threadQuery, &th, client.Var("id", root.ID))
	require.NotNil(t, th.Comment)
	require.Equal(t, replies[:2], nodeIDs(th.Comment.Replies.Edges))
	require.True(t, th.Comment.Replies.PageInfo.HasNextPage)

	c.MustPost(threadQuery, &th, client.Var("id", root.ID), client.Var("after", th.Comment.Replies.PageInfo.EndCursor))
	require.Equal(t, replies[2:], nodeIDs(th.Comment.Replies.Edges))
	require.False(t, th.Comment.Replies.PageInfo.HasNextPage)

	th = thread{}
	c.MustPost(threadQuery, &th, client.Var("id", root.ID+1000))
	require.Nil(t, th.Comment)
}

func nodeIDs(edges []struct{ Node struct{ ID int64 } }) []int64 {
	ids := make([]int64, 0, len(edges))
	for _, e := range edges {
		ids = append(ids, e.Node.ID)
	}
	return ids
}

func TestCommentAdded_Subscription(t *testing.T) {
	c, svc, _ := newClient(t)
	ctx := context.Background()
	newPost := func() int64 {
		p, err := svc.CreatePost(ctx, service.Post{AuthorID: "author", Title: "t", Body: "b", CommentsEnabled: true})
		require.NoError(t, err)
		return p.ID
	}
	postID, otherID := newPost(), newPost()

	sub := c.Websocket(`subscription($id: ID!) { commentAdded(postId: $id) { postId body } }`, client.Var("id", postID))
	defer func() { _ = sub.Close() }()

	// The server registers the subscription asynchronously after the start
	// message, so keep commenting until the first event arrives.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_, _ = svc.CreateComment(ctx, service.Comment{PostID: otherID, AuthorID: "bob", Body: "other post"})
				_, _ = svc.CreateComment(ctx, service.Comment{PostID: postID, AuthorID: "bob", Body: "live"})
			}
		}
	}()

	type event struct {
		CommentAdded struct {
			PostID int64
			Body   string
		}
	}
	got := make(chan event, 1)
	errs := make(chan error, 1)
	go func() {
		var e event
		if err := sub.Next(&e); err != nil {
			errs <- err
			return
		}
		got <- e
	}()

	select {
	case e := <-got:
		// Comments on the other post must not reach this subscriber.
		require.Equal(t, postID, e.CommentAdded.PostID)
		require.Equal(t, "live", e.CommentAdded.Body)
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("no event received")
	}
}

func TestComplexityLimit(t *testing.T) {
	c, _, _ := newClient(t)

	// A feed with comments and their first replies fits the limit with default page sizes.
	_, errs := post(t, c, `{ posts { edges { node { id title
		comments { edges { node { body replies { edges { node { body } } } } } }
	} } } }`)
	require.Empty(t, errs)

	// Three levels of replies under a post do not.
	_, errs = post(t, c, `{ post(id: 1) { comments { edges { node {
		replies { edges { node { replies { edges { node { replies { edges { node { id } } } } } } } } }
	} } } } }`)
	require.Len(t, errs, 1)
	require.Equal(t, "COMPLEXITY_LIMIT_EXCEEDED", errs[0].Extensions["code"])
}
