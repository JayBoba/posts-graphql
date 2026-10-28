package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/playground"

	"github.com/JayBoba/posts-graphql/internal/graph"
	"github.com/JayBoba/posts-graphql/internal/pubsub"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/memory"
	"github.com/JayBoba/posts-graphql/internal/storage/postgres"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run is separate from main so that deferred calls (closing the DB pool) run:
// log.Fatal exits the process without running them.
func run() error {
	storageType := flag.String("storage", envOr("STORAGE", "memory"), "storage backend: memory or postgres")
	port := flag.String("port", envOr("PORT", "8080"), "HTTP server port")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store service.Storage
	switch *storageType {
	case "memory":
		store = memory.New()
	case "postgres":
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return errors.New("DATABASE_URL is required for postgres storage")
		}
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		pg, err := postgres.New(connectCtx, dsn)
		cancel()
		if err != nil {
			return err
		}
		defer pg.Close()
		store = pg
	default:
		return fmt.Errorf("unknown storage %q (expected memory or postgres)", *storageType)
	}

	svc := service.New(store, pubsub.New())

	mux := http.NewServeMux()
	mux.Handle("/", playground.Handler("GraphQL playground", "/query"))
	mux.Handle("/query", graph.NewServer(svc))
	mux.Handle("GET /healthz", healthHandler(store))

	// Request contexts derive from baseCtx, not from the signal context: on SIGTERM
	// in-flight requests must be allowed to finish, so baseCtx is cancelled only
	// after Shutdown has drained them.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	httpSrv := &http.Server{
		Addr:              ":" + *port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout: subscriptions keep the WebSocket open.
		BaseContext: func(net.Listener) context.Context { return baseCtx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("storage=%s, playground: http://localhost:%s/", *storageType, *port)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := httpSrv.Shutdown(shutdownCtx)
	// Shutdown does not track hijacked WebSocket connections; cancelling the base
	// context ends the subscriptions so their clients are disconnected cleanly.
	cancelBase()
	return err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
