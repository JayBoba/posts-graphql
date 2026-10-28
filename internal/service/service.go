package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Service struct {
	storage Storage
	broker  Broker
}

func New(storage Storage, broker Broker) *Service {
	return &Service{storage: storage, broker: broker}
}

func (s *Service) CreatePost(ctx context.Context, p Post) (Post, error) {
	p.AuthorID = strings.TrimSpace(p.AuthorID)
	p.Title = strings.TrimSpace(p.Title)
	switch {
	case p.AuthorID == "":
		return Post{}, fmt.Errorf("%w: authorId is required", ErrInvalidInput)
	case p.Title == "":
		return Post{}, fmt.Errorf("%w: title is required", ErrInvalidInput)
	case strings.TrimSpace(p.Body) == "":
		return Post{}, fmt.Errorf("%w: body is required", ErrInvalidInput)
	}
	return s.storage.CreatePost(ctx, p)
}

func (s *Service) GetPost(ctx context.Context, id int64) (Post, error) {
	return s.storage.GetPost(ctx, id)
}

func (s *Service) ListPosts(ctx context.Context, after *int64, first int) (Page[Post], error) {
	limit, err := pageSize(first)
	if err != nil {
		return Page[Post]{}, err
	}
	posts, err := s.storage.ListPosts(ctx, after, limit+1)
	if err != nil {
		return Page[Post]{}, err
	}
	return makePage(posts, limit), nil
}

func (s *Service) SetCommentsEnabled(ctx context.Context, postID int64, authorID string, enabled bool) (Post, error) {
	post, err := s.storage.GetPost(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	if post.AuthorID != authorID {
		return Post{}, ErrForbidden
	}
	return s.storage.SetCommentsEnabled(ctx, postID, enabled)
}

func (s *Service) CreateComment(ctx context.Context, c Comment) (Comment, error) {
	c.AuthorID = strings.TrimSpace(c.AuthorID)
	switch {
	case c.AuthorID == "":
		return Comment{}, fmt.Errorf("%w: authorId is required", ErrInvalidInput)
	case strings.TrimSpace(c.Body) == "":
		return Comment{}, fmt.Errorf("%w: comment is empty", ErrInvalidInput)
	case utf8.RuneCountInString(c.Body) > MaxCommentLength:
		return Comment{}, fmt.Errorf("%w: comment is longer than %d characters", ErrInvalidInput, MaxCommentLength)
	}
	saved, err := s.storage.CreateComment(ctx, c)
	if err != nil {
		return Comment{}, err
	}
	s.broker.Publish(saved)
	return saved, nil
}

func (s *Service) ListComments(ctx context.Context, postID int64, parentID, after *int64, first int) (Page[Comment], error) {
	limit, err := pageSize(first)
	if err != nil {
		return Page[Comment]{}, err
	}
	comments, err := s.storage.ListComments(ctx, postID, parentID, after, limit+1)
	if err != nil {
		return Page[Comment]{}, err
	}
	return makePage(comments, limit), nil
}

func pageSize(first int) (int, error) {
	if first < 1 {
		return 0, fmt.Errorf("%w: first must be positive", ErrInvalidInput)
	}
	return min(first, MaxPageSize), nil
}

func makePage[T any](items []T, limit int) Page[T] {
	if len(items) > limit {
		return Page[T]{Items: items[:limit], HasNext: true}
	}
	return Page[T]{Items: items}
}

func (s *Service) SubscribeComments(ctx context.Context, postID int64) (<-chan *Comment, error) {
	if _, err := s.storage.GetPost(ctx, postID); err != nil {
		return nil, err
	}
	return s.broker.Subscribe(ctx, postID), nil
}

func (s *Service) GetComment(ctx context.Context, id int64) (Comment, error) {
	return s.storage.GetComment(ctx, id)
}

func (s *Service) ListRepliesBatch(ctx context.Context, parentIDs []int64, first int) (map[int64]Page[Comment], error) {
	limit, err := pageSize(first)
	if err != nil {
		return nil, err
	}
	byParent, err := s.storage.ListRepliesBatch(ctx, parentIDs, limit+1)
	if err != nil {
		return nil, err
	}
	res := make(map[int64]Page[Comment], len(parentIDs))
	for _, id := range parentIDs {
		res[id] = makePage(byParent[id], limit)
	}
	return res, nil
}