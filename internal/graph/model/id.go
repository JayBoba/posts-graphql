package model

import (
	"fmt"

	"github.com/99designs/gqlgen/graphql"

	"github.com/JayBoba/posts-graphql/internal/service"
)

// MarshalID and UnmarshalID bind the GraphQL ID scalar to int64 (see gqlgen.yml).
// The built-in Int64 scalar would return a raw strconv error for an id like "abc";
// here it becomes a regular BAD_USER_INPUT error.

func MarshalID(id int64) graphql.Marshaler {
	return graphql.MarshalInt64(id)
}

func UnmarshalID(v any) (int64, error) {
	id, err := graphql.UnmarshalInt64(v)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid id %v", service.ErrInvalidInput, v)
	}
	return id, nil
}
