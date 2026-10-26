package graph

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/JayBoba/posts-graphql/internal/graph/model"
	"github.com/JayBoba/posts-graphql/internal/service"
)

func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCursor(cursor *string) (*int64, error) {
	if cursor == nil {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", service.ErrInvalidInput)
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("%w: invalid cursor", service.ErrInvalidInput)
	}
	return &id, nil
}

func pageSize(first *int32, def int) int {
	if first == nil {
		return def
	}
	return int(*first)
}

func postConnection(p service.Page[service.Post]) *model.PostConnection {
	conn := &model.PostConnection{
		Edges:    make([]*model.PostEdge, 0, len(p.Items)),
		PageInfo: &model.PageInfo{HasNextPage: p.HasNext},
	}
	for i := range p.Items {
		post := &p.Items[i]
		conn.Edges = append(conn.Edges, &model.PostEdge{Cursor: encodeCursor(post.ID), Node: post})
	}
	if n := len(conn.Edges); n > 0 {
		conn.PageInfo.EndCursor = &conn.Edges[n-1].Cursor
	}
	return conn
}

func commentConnection(p service.Page[service.Comment]) *model.CommentConnection {
	conn := &model.CommentConnection{
		Edges:    make([]*model.CommentEdge, 0, len(p.Items)),
		PageInfo: &model.PageInfo{HasNextPage: p.HasNext},
	}
	for i := range p.Items {
		c := &p.Items[i]
		conn.Edges = append(conn.Edges, &model.CommentEdge{Cursor: encodeCursor(c.ID), Node: c})
	}
	if n := len(conn.Edges); n > 0 {
		conn.PageInfo.EndCursor = &conn.Edges[n-1].Cursor
	}
	return conn
}
