package graph

import (
	"context"
	"net/http"
	"time"

	"github.com/vikstrous/dataloadgen"

	"github.com/JayBoba/posts-graphql/internal/service"
)

type repliesKey struct {
	parentID int64
	first    int
}

type loaders struct {
	replies *dataloadgen.Loader[repliesKey, service.Page[service.Comment]]
}

type loadersCtxKey struct{}

func LoadersMiddleware(svc *service.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := &loaders{
			replies: dataloadgen.NewLoader(
				func(ctx context.Context, keys []repliesKey) ([]service.Page[service.Comment], []error) {
					return loadReplies(ctx, svc, keys)
				},
				dataloadgen.WithWait(time.Millisecond),
			),
		}
		ctx := context.WithValue(r.Context(), loadersCtxKey{}, l)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loadersFrom(ctx context.Context) *loaders {
	l, _ := ctx.Value(loadersCtxKey{}).(*loaders)
	return l
}

func loadReplies(ctx context.Context, svc *service.Service, keys []repliesKey) ([]service.Page[service.Comment], []error) {
	idsByFirst := make(map[int][]int64)
	for _, k := range keys {
		idsByFirst[k.first] = append(idsByFirst[k.first], k.parentID)
	}

	pages := make(map[repliesKey]service.Page[service.Comment], len(keys))
	for first, ids := range idsByFirst {
		byParent, err := svc.ListRepliesBatch(ctx, ids, first)
		if err != nil {
			errs := make([]error, len(keys))
			for i := range errs {
				errs[i] = err
			}
			return make([]service.Page[service.Comment], len(keys)), errs
		}
		for id, p := range byParent {
			pages[repliesKey{parentID: id, first: first}] = p
		}
	}
	res := make([]service.Page[service.Comment], len(keys))
	for i, k := range keys {
		res[i] = pages[k]
	}
	return res, nil
}
