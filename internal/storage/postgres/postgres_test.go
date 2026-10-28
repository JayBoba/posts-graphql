package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/storagetest"
)

// TestStorage needs a running PostgreSQL and is skipped without TEST_DATABASE_URL.
// The tests truncate the tables, so do not point it at a database with real data.
func TestStorage(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	s, err := New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(s.Close)

	storagetest.Run(t, func(t *testing.T) service.Storage {
		_, err := s.pool.Exec(context.Background(), `TRUNCATE posts, comments RESTART IDENTITY`)
		require.NoError(t, err)
		return s
	})
}
