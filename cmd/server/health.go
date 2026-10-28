package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// pinger is implemented by storages that depend on an external service.
type pinger interface {
	Ping(ctx context.Context) error
}

// healthHandler answers 200 when the service can serve requests and 503 when
// its storage is unreachable, so an orchestrator can restart it or stop routing
// traffic to it.
func healthHandler(store any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := store.(pinger); ok {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := p.Ping(ctx); err != nil {
				slog.ErrorContext(ctx, "health check failed", "err", err)
				http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = w.Write([]byte("ok"))
	})
}
