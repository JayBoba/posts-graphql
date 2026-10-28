package memory_test

import (
	"testing"

	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/JayBoba/posts-graphql/internal/storage/memory"
	"github.com/JayBoba/posts-graphql/internal/storage/storagetest"
)

func TestStorage(t *testing.T) {
	storagetest.Run(t, func(*testing.T) service.Storage { return memory.New() })
}
