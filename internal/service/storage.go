package service

import (
	"context"

	"github.com/JayBoba/posts-graphql/internal/domain"
)

// Storage is declared here, by its consumer. Implementations satisfy it
// implicitly and depend only on the domain package, not on the service.
type Storage interface {
	CreatePost(ctx context.Context, p domain.Post) (domain.Post, error)
	GetPost(ctx context.Context, id int64) (domain.Post, error)
	ListPosts(ctx context.Context, after *int64, limit int) ([]domain.Post, error)
	SetCommentsEnabled(ctx context.Context, postID int64, enabled bool) (domain.Post, error)

	// CreateComment must check, atomically with the insert, that the post exists
	// (domain.ErrPostNotFound), that its comments are enabled (domain.ErrCommentsDisabled)
	// and that the parent, if any, belongs to the same post (domain.ErrParentNotFound).
	// Checking in the service and inserting afterwards would race with the author
	// disabling comments in between, so the rule is enforced where the data lives.
	CreateComment(ctx context.Context, c domain.Comment) (domain.Comment, error)
	ListComments(ctx context.Context, postID int64, parentID *int64, after *int64, limit int) ([]domain.Comment, error)
	GetComment(ctx context.Context, id int64) (domain.Comment, error)
	ListRepliesBatch(ctx context.Context, parentIDs []int64, limit int) (map[int64][]domain.Comment, error)
}

// Broker delivers newly created comments to the subscribers of their post.
type Broker interface {
	Publish(c domain.Comment)
	Subscribe(ctx context.Context, postID int64) <-chan *domain.Comment
}
