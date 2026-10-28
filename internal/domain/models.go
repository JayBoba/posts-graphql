// Package domain holds the entities, their limits and the domain errors shared
// by the service, the storages and the GraphQL layer. It depends on nothing else
// in the project.
package domain

import "time"

// Text limits, counted in characters (runes), not bytes.
const (
	MaxAuthorIDLength  = 100
	MaxCommentLength   = 2000
	MaxPostTitleLength = 300
	MaxPostBodyLength  = 100_000
)

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
