package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

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
