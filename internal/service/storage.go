package service

import "context"

type Storage interface {
	CreatePost(ctx context.Context, p Post) (Post, error)
	GetPost(ctx context.Context, id int64) (Post, error)
	ListPosts(ctx context.Context, after *int64, limit int) ([]Post, error)
	SetCommentsEnabled(ctx context.Context, postID int64, enabled bool) (Post, error)
	CreateComment(ctx context.Context, c Comment) (Comment, error)
	ListComments(ctx context.Context, postID int64, parentID *int64, after *int64, limit int) ([]Comment, error)
}

type Broker interface {
	Publish(c Comment)
	Subscribe(ctx context.Context, postID int64) <-chan *Comment
}