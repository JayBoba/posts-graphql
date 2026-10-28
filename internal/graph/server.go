package graph

import (
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/JayBoba/posts-graphql/internal/service"
)

// NewServer builds the GraphQL handler with all middleware. It lives outside main
// so that tests run against exactly the same configuration as production.
func NewServer(svc *service.Service) *handler.Server {
	srv := handler.New(NewExecutableSchema(NewConfig(svc)))
	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
	})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.Use(extension.Introspection{})
	srv.Use(extension.FixedComplexityLimit(ComplexityLimit))
	srv.AroundOperations(WithLoaders(svc))
	srv.AroundFields(MarkInternalErrors)
	srv.SetErrorPresenter(ErrorPresenter)
	return srv
}
