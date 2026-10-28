package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/JayBoba/posts-graphql/internal/domain"
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

func (s *Service) CreatePost(ctx context.Context, p domain.Post) (domain.Post, error) {
	p.AuthorID = strings.TrimSpace(p.AuthorID)
	p.Title = strings.TrimSpace(p.Title)
	switch {
	case p.AuthorID == "":
		return domain.Post{}, fmt.Errorf("%w: authorId is required", domain.ErrInvalidInput)
	case p.Title == "":
		return domain.Post{}, fmt.Errorf("%w: title is required", domain.ErrInvalidInput)
	case utf8.RuneCountInString(p.Title) > domain.MaxPostTitleLength:
		return domain.Post{}, fmt.Errorf("%w: title is longer than %d characters", domain.ErrInvalidInput, domain.MaxPostTitleLength)
	case strings.TrimSpace(p.Body) == "":
		return domain.Post{}, fmt.Errorf("%w: body is required", domain.ErrInvalidInput)
	case utf8.RuneCountInString(p.Body) > domain.MaxPostBodyLength:
		return domain.Post{}, fmt.Errorf("%w: body is longer than %d characters", domain.ErrInvalidInput, domain.MaxPostBodyLength)
	}
	return s.storage.CreatePost(ctx, p)
}

func (s *Service) GetPost(ctx context.Context, id int64) (domain.Post, error) {
	return s.storage.GetPost(ctx, id)
}

func (s *Service) ListPosts(ctx context.Context, after *int64, first int) (domain.Page[domain.Post], error) {
	limit, err := pageSize(first)
	if err != nil {
		return domain.Page[domain.Post]{}, err
	}
	posts, err := s.storage.ListPosts(ctx, after, limit+1)
	if err != nil {
		return domain.Page[domain.Post]{}, err
	}
	return makePage(posts, limit), nil
}

func (s *Service) SetCommentsEnabled(ctx context.Context, postID int64, authorID string, enabled bool) (domain.Post, error) {
	post, err := s.storage.GetPost(ctx, postID)
	if err != nil {
		return domain.Post{}, err
	}
	// authorId is trimmed on creation, so it is compared the same way here.
	if post.AuthorID != strings.TrimSpace(authorID) {
		return domain.Post{}, domain.ErrForbidden
	}
	return s.storage.SetCommentsEnabled(ctx, postID, enabled)
}

func (s *Service) CreateComment(ctx context.Context, c domain.Comment) (domain.Comment, error) {
	c.AuthorID = strings.TrimSpace(c.AuthorID)
	switch {
	case c.AuthorID == "":
		return domain.Comment{}, fmt.Errorf("%w: authorId is required", domain.ErrInvalidInput)
	case strings.TrimSpace(c.Body) == "":
		return domain.Comment{}, fmt.Errorf("%w: comment is empty", domain.ErrInvalidInput)
	case utf8.RuneCountInString(c.Body) > domain.MaxCommentLength:
		return domain.Comment{}, fmt.Errorf("%w: comment is longer than %d characters", domain.ErrInvalidInput, domain.MaxCommentLength)
	}
	saved, err := s.storage.CreateComment(ctx, c)
	if err != nil {
		return domain.Comment{}, err
	}
	s.broker.Publish(saved)
	return saved, nil
}

func (s *Service) ListComments(ctx context.Context, postID int64, parentID, after *int64, first int) (domain.Page[domain.Comment], error) {
	limit, err := pageSize(first)
	if err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	comments, err := s.storage.ListComments(ctx, postID, parentID, after, limit+1)
	if err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	return makePage(comments, limit), nil
}

func pageSize(first int) (int, error) {
	if first < 1 {
		return 0, fmt.Errorf("%w: first must be positive", domain.ErrInvalidInput)
	}
	return min(first, MaxPageSize), nil
}

func makePage[T any](items []T, limit int) domain.Page[T] {
	if len(items) > limit {
		return domain.Page[T]{Items: items[:limit], HasNext: true}
	}
	return domain.Page[T]{Items: items}
}

func (s *Service) SubscribeComments(ctx context.Context, postID int64) (<-chan *domain.Comment, error) {
	if _, err := s.storage.GetPost(ctx, postID); err != nil {
		return nil, err
	}
	return s.broker.Subscribe(ctx, postID), nil
}

func (s *Service) GetComment(ctx context.Context, id int64) (domain.Comment, error) {
	return s.storage.GetComment(ctx, id)
}

func (s *Service) ListRepliesBatch(ctx context.Context, parentIDs []int64, first int) (map[int64]domain.Page[domain.Comment], error) {
	limit, err := pageSize(first)
	if err != nil {
		return nil, err
	}
	byParent, err := s.storage.ListRepliesBatch(ctx, parentIDs, limit+1)
	if err != nil {
		return nil, err
	}
	res := make(map[int64]domain.Page[domain.Comment], len(parentIDs))
	for _, id := range parentIDs {
		res[id] = makePage(byParent[id], limit)
	}
	return res, nil
}
