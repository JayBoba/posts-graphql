package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/JayBoba/posts-graphql/internal/service"
)

type childKey struct {
	postID   int64
	parentID int64
}

type Storage struct {
	mu sync.RWMutex

	lastPostID    int64
	lastCommentID int64

	posts   map[int64]service.Post
	postIDs []int64

	comments map[int64]service.Comment
	children map[childKey][]int64
}

func New() *Storage {
	return &Storage{
		posts:    make(map[int64]service.Post),
		comments: make(map[int64]service.Comment),
		children: make(map[childKey][]int64),
	}
}

var _ service.Storage = (*Storage)(nil)

func (s *Storage) CreatePost(_ context.Context, p service.Post) (service.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastPostID++
	p.ID = s.lastPostID
	p.CreatedAt = time.Now().UTC()
	s.posts[p.ID] = p
	s.postIDs = append(s.postIDs, p.ID)
	return p, nil
}

func (s *Storage) GetPost(_ context.Context, id int64) (service.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.posts[id]
	if !ok {
		return service.Post{}, service.ErrPostNotFound
	}
	return p, nil
}

func (s *Storage) ListPosts(_ context.Context, after *int64, limit int) ([]service.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	end := len(s.postIDs)
	if after != nil {
		end = sort.Search(len(s.postIDs), func(i int) bool { return s.postIDs[i] >= *after })
	}

	res := make([]service.Post, 0, min(limit, end))
	for i := end - 1; i >= 0 && len(res) < limit; i-- {
		res = append(res, s.posts[s.postIDs[i]])
	}
	return res, nil
}

func (s *Storage) SetCommentsEnabled(_ context.Context, postID int64, enabled bool) (service.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.posts[postID]
	if !ok {
		return service.Post{}, service.ErrPostNotFound
	}
	p.CommentsEnabled = enabled
	s.posts[postID] = p
	return p, nil
}

func (s *Storage) CreateComment(_ context.Context, c service.Comment) (service.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	post, ok := s.posts[c.PostID]
	if !ok {
		return service.Comment{}, service.ErrPostNotFound
	}
	if !post.CommentsEnabled {
		return service.Comment{}, service.ErrCommentsDisabled
	}

	key := childKey{postID: c.PostID}
	if c.ParentID != nil {
		parent, ok := s.comments[*c.ParentID]
		if !ok || parent.PostID != c.PostID {
			return service.Comment{}, service.ErrParentNotFound
		}
		key.parentID = parent.ID
	}

	s.lastCommentID++
	c.ID = s.lastCommentID
	c.CreatedAt = time.Now().UTC()
	s.comments[c.ID] = c
	s.children[key] = append(s.children[key], c.ID)
	return c, nil
}

func (s *Storage) ListComments(_ context.Context, postID int64, parentID *int64, after *int64, limit int) ([]service.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := childKey{postID: postID}
	if parentID != nil {
		key.parentID = *parentID
	}
	ids := s.children[key]

	start := 0
	if after != nil {
		start = sort.Search(len(ids), func(i int) bool { return ids[i] > *after })
	}

	end := min(start+limit, len(ids))
	res := make([]service.Comment, 0, end-start)
	for _, id := range ids[start:end] {
		res = append(res, s.comments[id])
	}
	return res, nil
}

func (s *Storage) GetComment(_ context.Context, id int64) (service.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.comments[id]
	if !ok {
		return service.Comment{}, service.ErrCommentNotFound
	}
	return c, nil
}
