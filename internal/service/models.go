package service

import "time"

const MaxCommentLength = 2000

type Post struct {
	ID              int64
	AuthorID        string
	Title           string
	Body            string
	CommentsEnabled bool
	CreatedAt       time.Time
}

type Comment struct {
	ID        int64
	PostID    int64
	ParentID  *int64
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

type Page[T any] struct {
	Items   []T
	HasNext bool
}
