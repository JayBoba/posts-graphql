package service

import "errors"

var (
	ErrPostNotFound     = errors.New("post not found")
	ErrParentNotFound   = errors.New("parent comment not found in this post")
	ErrCommentsDisabled = errors.New("comments are disabled for this post")
	ErrForbidden        = errors.New("only the post author can do this")
	ErrInvalidInput     = errors.New("invalid input")
)
