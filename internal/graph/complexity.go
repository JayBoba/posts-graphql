package graph

import "github.com/JayBoba/posts-graphql/internal/service"

const defaultRepliesPageSize = 10

// ComplexityLimit is calibrated against default page sizes: it lets through
// a post feed with comments and their first replies (~14 000) and a single
// post with two levels of replies, but rejects deeper or wider trees.
const ComplexityLimit = 20000

func paged(def int) func(childComplexity int, first *int32, after *string) int {
	return func(childComplexity int, first *int32, after *string) int {
		n := def
		if first != nil && *first > 0 {
			n = int(*first)
		}
		return 1 + childComplexity*min(n, service.MaxPageSize)
	}
}

func NewConfig(svc *service.Service) Config {
	cfg := Config{Resolvers: &Resolver{Svc: svc}}
	cfg.Complexity.Query.Posts = paged(service.DefaultPageSize)
	cfg.Complexity.Post.Comments = paged(service.DefaultPageSize)
	cfg.Complexity.Comment.Replies = paged(defaultRepliesPageSize)
	return cfg
}
