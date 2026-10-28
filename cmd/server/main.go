package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/JayBoba/posts-graphql/internal/graph"
	"github.com/JayBoba/posts-graphql/internal/pubsub"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/memory"
	"github.com/JayBoba/posts-graphql/internal/storage/postgres"
)

func main() {
	storageType := flag.String("storage", envOr("STORAGE", "memory"), "хранилище: memory или postgres")
	port := flag.String("port", envOr("PORT", "8080"), "порт HTTP-сервера")
	flag.Parse()

	var store service.Storage
	switch *storageType {
	case "memory":
		store = memory.New()
	case "postgres":
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			log.Fatal("DATABASE_URL is required for postgres storage")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pg, err := postgres.New(ctx, dsn)
		cancel()
		if err != nil {
			log.Fatal(err)
		}
		defer pg.Close()
		store = pg
	default:
		log.Fatalf("unknown storage %q (expected memory or postgres)", *storageType)
	}

	svc := service.New(store, pubsub.New())

	srv := handler.New(graph.NewExecutableSchema(graph.NewConfig(svc)))
	srv.Use(extension.FixedComplexityLimit(graph.ComplexityLimit))
	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
	})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.Use(extension.Introspection{})
	srv.SetErrorPresenter(graph.ErrorPresenter)

	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", srv)

	log.Printf("storage=%s, playground: http://localhost:%s/", *storageType, *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
