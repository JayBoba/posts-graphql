// Package storagetest holds contract tests that every service.Storage
// implementation must pass, so the in-memory and PostgreSQL storages are
// guaranteed to behave the same way.
package storagetest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/domain"
	"github.com/JayBoba/posts-graphql/internal/service"
)

// Run runs the contract tests. newStorage must return an empty storage
// for every call.
func Run(t *testing.T, newStorage func(t *testing.T) service.Storage) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s service.Storage)
	}{
		{"CreateAndGetPost", testCreateAndGetPost},
		{"ListPosts", testListPosts},
		{"SetCommentsEnabled", testSetCommentsEnabled},
		{"CreateCommentRules", testCreateCommentRules},
		{"ListComments", testListComments},
		{"GetComment", testGetComment},
		{"ListRepliesBatch", testListRepliesBatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, newStorage(t))
		})
	}
}

func createPost(t *testing.T, s service.Storage, enabled bool) domain.Post {
	t.Helper()
	p, err := s.CreatePost(context.Background(), domain.Post{
		AuthorID: "author", Title: "title", Body: "body", CommentsEnabled: enabled,
	})
	require.NoError(t, err)
	return p
}

func createComment(t *testing.T, s service.Storage, postID int64, parentID *int64) domain.Comment {
	t.Helper()
	c, err := s.CreateComment(context.Background(), domain.Comment{
		PostID: postID, ParentID: parentID, AuthorID: "bob", Body: "text",
	})
	require.NoError(t, err)
	return c
}

func ids[T any](items []T, id func(T) int64) []int64 {
	res := make([]int64, 0, len(items))
	for _, it := range items {
		res = append(res, id(it))
	}
	return res
}

func postID(p domain.Post) int64       { return p.ID }
func commentID(c domain.Comment) int64 { return c.ID }

func testCreateAndGetPost(t *testing.T, s service.Storage) {
	ctx := context.Background()
	created := createPost(t, s, true)
	require.Positive(t, created.ID)
	require.False(t, created.CreatedAt.IsZero())

	got, err := s.GetPost(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "author", got.AuthorID)
	require.Equal(t, "title", got.Title)
	require.Equal(t, "body", got.Body)
	require.True(t, got.CommentsEnabled)

	_, err = s.GetPost(ctx, created.ID+1000)
	require.ErrorIs(t, err, domain.ErrPostNotFound)
}

func testListPosts(t *testing.T, s service.Storage) {
	ctx := context.Background()
	var all []int64
	for range 5 {
		all = append(all, createPost(t, s, true).ID)
	}

	// Newest first.
	page, err := s.ListPosts(ctx, nil, 3)
	require.NoError(t, err)
	require.Equal(t, []int64{all[4], all[3], all[2]}, ids(page, postID))

	// Strictly after the cursor, up to the end.
	page, err = s.ListPosts(ctx, &all[2], 10)
	require.NoError(t, err)
	require.Equal(t, []int64{all[1], all[0]}, ids(page, postID))

	page, err = s.ListPosts(ctx, &all[0], 10)
	require.NoError(t, err)
	require.Empty(t, page)
}

func testSetCommentsEnabled(t *testing.T, s service.Storage) {
	ctx := context.Background()
	p := createPost(t, s, true)

	updated, err := s.SetCommentsEnabled(ctx, p.ID, false)
	require.NoError(t, err)
	require.False(t, updated.CommentsEnabled)

	got, err := s.GetPost(ctx, p.ID)
	require.NoError(t, err)
	require.False(t, got.CommentsEnabled)

	_, err = s.SetCommentsEnabled(ctx, p.ID+1000, true)
	require.ErrorIs(t, err, domain.ErrPostNotFound)
}

func testCreateCommentRules(t *testing.T, s service.Storage) {
	ctx := context.Background()
	open := createPost(t, s, true)
	closed := createPost(t, s, false)
	other := createPost(t, s, true)
	root := createComment(t, s, open.ID, nil)
	missing := root.ID + 1000

	tests := []struct {
		name     string
		postID   int64
		parentID *int64
		wantErr  error
	}{
		{"top-level comment", open.ID, nil, nil},
		{"reply", open.ID, &root.ID, nil},
		{"post not found", open.ID + 1000, nil, domain.ErrPostNotFound},
		{"comments disabled", closed.ID, nil, domain.ErrCommentsDisabled},
		{"parent not found", open.ID, &missing, domain.ErrParentNotFound},
		{"parent from another post", other.ID, &root.ID, domain.ErrParentNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := s.CreateComment(ctx, domain.Comment{
				PostID: tt.postID, ParentID: tt.parentID, AuthorID: "bob", Body: "text",
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Positive(t, c.ID)
			require.Equal(t, tt.postID, c.PostID)
			require.Equal(t, tt.parentID, c.ParentID)
			require.False(t, c.CreatedAt.IsZero())
		})
	}
}

func testListComments(t *testing.T, s service.Storage) {
	ctx := context.Background()
	p := createPost(t, s, true)
	other := createPost(t, s, true)

	var roots []int64
	for range 3 {
		roots = append(roots, createComment(t, s, p.ID, nil).ID)
	}
	var replies []int64
	for range 3 {
		replies = append(replies, createComment(t, s, p.ID, &roots[0]).ID)
	}
	createComment(t, s, other.ID, nil)

	// Only top-level comments of this post, oldest first.
	page, err := s.ListComments(ctx, p.ID, nil, nil, 10)
	require.NoError(t, err)
	require.Equal(t, roots, ids(page, commentID))

	// Limit and cursor.
	page, err = s.ListComments(ctx, p.ID, nil, nil, 2)
	require.NoError(t, err)
	require.Equal(t, roots[:2], ids(page, commentID))

	page, err = s.ListComments(ctx, p.ID, nil, &roots[1], 10)
	require.NoError(t, err)
	require.Equal(t, roots[2:], ids(page, commentID))

	// Direct replies of one comment.
	page, err = s.ListComments(ctx, p.ID, &roots[0], &replies[0], 10)
	require.NoError(t, err)
	require.Equal(t, replies[1:], ids(page, commentID))

	page, err = s.ListComments(ctx, p.ID, &roots[1], nil, 10)
	require.NoError(t, err)
	require.Empty(t, page)
}

func testGetComment(t *testing.T, s service.Storage) {
	ctx := context.Background()
	p := createPost(t, s, true)
	root := createComment(t, s, p.ID, nil)
	reply := createComment(t, s, p.ID, &root.ID)

	got, err := s.GetComment(ctx, reply.ID)
	require.NoError(t, err)
	require.Equal(t, reply.ID, got.ID)
	require.Equal(t, p.ID, got.PostID)
	require.Equal(t, &root.ID, got.ParentID)
	require.Equal(t, "bob", got.AuthorID)
	require.Equal(t, "text", got.Body)

	_, err = s.GetComment(ctx, reply.ID+1000)
	require.ErrorIs(t, err, domain.ErrCommentNotFound)
}

func testListRepliesBatch(t *testing.T, s service.Storage) {
	ctx := context.Background()
	p := createPost(t, s, true)
	a := createComment(t, s, p.ID, nil)
	b := createComment(t, s, p.ID, nil)
	empty := createComment(t, s, p.ID, nil)

	var aReplies, bReplies []int64
	for range 3 {
		aReplies = append(aReplies, createComment(t, s, p.ID, &a.ID).ID)
		bReplies = append(bReplies, createComment(t, s, p.ID, &b.ID).ID)
	}

	// The limit applies to each parent separately, not to the whole batch.
	got, err := s.ListRepliesBatch(ctx, []int64{a.ID, b.ID, empty.ID, a.ID + 1000}, 2)
	require.NoError(t, err)
	require.Equal(t, aReplies[:2], ids(got[a.ID], commentID))
	require.Equal(t, bReplies[:2], ids(got[b.ID], commentID))
	require.Empty(t, got[empty.ID])
	require.Empty(t, got[a.ID+1000])
}
