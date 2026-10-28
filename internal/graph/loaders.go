package graph

import (
	"context"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vikstrous/dataloadgen"

	"github.com/JayBoba/posts-graphql/internal/domain"
	"github.com/JayBoba/posts-graphql/internal/service"
)

type repliesKey struct {
	parentID int64
	first    int
}

type loaders struct {
	replies *dataloadgen.Loader[repliesKey, domain.Page[domain.Comment]]
}

type loadersCtxKey struct{}

// WithLoaders is an operation middleware that gives every GraphQL operation its
// own set of dataloaders with its own cache. An HTTP middleware would not work:
// over WebSocket one connection carries many operations, so the cache would live
// as long as the socket and serve stale data.
func WithLoaders(svc *service.Service) graphql.OperationMiddleware {
	return func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		l := &loaders{
			replies: dataloadgen.NewLoader(
				func(ctx context.Context, keys []repliesKey) ([]domain.Page[domain.Comment], []error) {
					return loadReplies(ctx, svc, keys)
				},
				dataloadgen.WithWait(time.Millisecond),
			),
		}
		return next(context.WithValue(ctx, loadersCtxKey{}, l))
	}
}

func loadersFrom(ctx context.Context) *loaders {
	l, _ := ctx.Value(loadersCtxKey{}).(*loaders)
	return l
}

func loadReplies(ctx context.Context, svc *service.Service, keys []repliesKey) ([]domain.Page[domain.Comment], []error) {
	idsByFirst := make(map[int][]int64)
	for _, k := range keys {
		idsByFirst[k.first] = append(idsByFirst[k.first], k.parentID)
	}

	// A failed group (e.g. first: 0) must fail only its own keys, not the
	// other page sizes requested in the same batch.
	pages := make(map[repliesKey]domain.Page[domain.Comment], len(keys))
	groupErrs := make(map[int]error)
	for first, ids := range idsByFirst {
		byParent, err := svc.ListRepliesBatch(ctx, ids, first)
		if err != nil {
			groupErrs[first] = err
			continue
		}
		for id, p := range byParent {
			pages[repliesKey{parentID: id, first: first}] = p
		}
	}

	res := make([]domain.Page[domain.Comment], len(keys))
	var errs []error
	if len(groupErrs) > 0 {
		errs = make([]error, len(keys))
	}
	for i, k := range keys {
		res[i] = pages[k]
		if errs != nil {
			errs[i] = groupErrs[k.first]
		}
	}
	return res, errs
}
