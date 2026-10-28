package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/storage/memory"
)

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name  string
		store any
		want  int
	}{
		{"in-memory storage", memory.New(), http.StatusOK},
		{"database is up", fakePinger{}, http.StatusOK},
		{"database is down", fakePinger{err: errors.New("connection refused")}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
			healthHandler(tt.store).ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}
