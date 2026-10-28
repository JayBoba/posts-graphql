# Posts & Comments (GraphQL, Go)

A GraphQL service for posts and threaded comments, similar to comment sections on Habr or Reddit.

- List posts and view a post with its comments.
- A post's author can disable comments.
- Comments nest without a depth limit and are limited to 2000 characters.
- Cursor-based pagination for posts, comments and replies.
- Subscriptions to new comments on a post (GraphQL Subscriptions).
- Storage is in-memory or PostgreSQL, selected by a startup parameter.

## Quick start

**PostgreSQL (docker compose):**
```bash
docker compose up --build
```
Playground: http://localhost:8080

**In-memory (Docker):**
```bash
docker build -t posts-graphql .
docker run --rm -p 8080:8080 posts-graphql
```

**Locally (Go 1.26+):**
```bash
go run ./cmd/server -storage=memory
```

After changing `internal/graph/schema.graphqls`, regenerate the GraphQL code:
```bash
go tool gqlgen generate
```

**Tests:**
```bash
go test ./...
# with the race detector (requires cgo; easiest in a container):
docker run --rm -v "$PWD:/app" -w /app golang:1.26 go test -race ./...
```

The PostgreSQL storage tests are skipped unless `TEST_DATABASE_URL` is set. They truncate the tables, so use a throwaway database:
```bash
docker run -d --rm --name posts-test-pg -p 55432:5432 \
  -e POSTGRES_USER=posts -e POSTGRES_PASSWORD=posts -e POSTGRES_DB=posts postgres:17-alpine
TEST_DATABASE_URL="postgres://posts:posts@localhost:55432/posts?sslmode=disable" go test ./internal/storage/postgres
```

## Configuration

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `-storage` | `STORAGE` | `memory` | `memory` or `postgres` |
| `-port` | `PORT` | `8080` | HTTP server port |
| — | `DATABASE_URL` | — | PostgreSQL connection string (required for `postgres`) |

The database schema is applied automatically on startup.

## Example queries

Create a post:
```graphql
mutation {
  createPost(input: {authorId: "alice", title: "First post", body: "Hello!"}) { id }
}
```

Comment on a post and reply to a comment:
```graphql
mutation { createComment(input: {postId: 1, authorId: "bob", body: "Top-level comment"}) { id } }
mutation { createComment(input: {postId: 1, parentId: 1, authorId: "carol", body: "Reply"}) { id } }
```

A post with comments and replies:
```graphql
query {
  post(id: 1) {
    title
    commentsEnabled
    comments(first: 20) {
      edges {
        node {
          id
          body
          replies(first: 10) {
            edges { node { id body } }
            pageInfo { hasNextPage endCursor }
          }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}
```

Next page: `comments(first: 20, after: "<endCursor>")`.
Continue a deep thread: `comment(id: 42) { replies { ... } }`.

Disable comments (post author only):
```graphql
mutation { setCommentsEnabled(postId: 1, authorId: "alice", enabled: false) { commentsEnabled } }
```

Subscribe to new comments (WebSocket):
```graphql
subscription { commentAdded(postId: 1) { id parentId authorId body } }
```

## Architecture

```
cmd/server/            entry point: configuration, storage selection, graceful shutdown
internal/graph/        GraphQL: server setup, schema, resolvers, pagination, dataloader, errors, complexity limit
internal/service/      business logic, validation, domain models and errors, Storage and Broker interfaces
internal/storage/
  memory/              in-memory storage
  postgres/            PostgreSQL (pgx) + database schema
internal/pubsub/       delivers new comments to subscribers
```

A request flows `graph → service → storage`. The service depends only on the `Storage` and `Broker` interfaces declared in the `service` package, so storages are interchangeable and can be replaced in tests.

## Key decisions

**Comment tree storage.** An adjacency list: each comment has a `parent_id` (`NULL` for top-level comments). Comments are only appended and never moved, and they are read level by level, so recursive queries are not needed. If loading a whole subtree at once becomes necessary, a materialized path can be added.

**Level-by-level loading.** `post.comments` returns top-level comments, `comment.replies` returns direct replies, and each level is paginated separately. Nesting depth does not inflate the response: clients load deep threads via `comment(id)`, like "load more replies" on Reddit.

**Pagination.** Cursor-based, in Relay format (`edges / pageInfo`). The cursor is an opaque string (base64-encoded id). Ids increase in creation order, so "after the cursor" means "newer". Offset pagination is not used: pages shift when new items are inserted, and large offsets are slow. To determine `hasNextPage`, the storage is asked for `limit + 1` items, with no separate `COUNT`. Posts are ordered newest first, comments oldest first. The default page size is 20 (10 for replies), the maximum is 100.

**PostgreSQL indexes.** Built for the two main queries: a partial index `(post_id, id) WHERE parent_id IS NULL` for top-level comments and `(parent_id, id)` for replies. A page is read straight from the index without sorting all of a post's comments.

**N+1.** The `replies` field is requested for every comment on a level. The first page of replies is loaded through a dataloader: replies for all comments on the level are collected into a single storage call (in PostgreSQL, `unnest` + `LATERAL ... LIMIT`, which reads only the needed index rows for each parent). Loaders are created per GraphQL operation rather than per HTTP request: over WebSocket one connection carries many operations, and a per-connection cache would serve stale data.

**Expensive queries.** A query complexity limit (`10000`): each field costs 1, and a paginated list costs the item cost × `first`. With default page sizes this allows top-level comments plus two levels of replies per query; a query that is too deep or too wide is rejected before execution.

**Concurrency.** The checks "the post exists, comments are enabled, the parent belongs to the same post" run atomically with the insert: in PostgreSQL as a single `INSERT … SELECT … WHERE EXISTS` statement, in memory under one mutex. The in-memory storage uses an `RWMutex` (reads vastly outnumber writes) and stores values rather than pointers in its maps, so data cannot be modified without holding the lock.

**Subscriptions.** New comments are published only after they are saved. Each subscriber has a buffered channel and non-blocking sends: a slow client may miss events but never delays others. When a client disconnects, its subscription is removed and its channel is closed, so goroutines do not leak.

**Errors.** The service returns domain errors, and an `ErrorPresenter` maps them in one place to GraphQL errors with `extensions.code`:

| Case | code |
|---|---|
| Invalid input (empty text, > 2000 characters, invalid cursor, `first < 1`) | `BAD_USER_INPUT` |
| Post, comment or parent not found | `NOT_FOUND` |
| Comments disabled, action by someone other than the author | `FORBIDDEN` |
| Internal error (e.g. the database is unavailable) | `INTERNAL` (details go to the log only) |
| Query too complex | `COMPLEXITY_LIMIT_EXCEEDED` |

gqlgen wraps every resolver error in a `*gqlerror.Error`, so by the time the presenter runs, a database error looks the same as an argument parsing error. A field middleware therefore marks non-domain resolver errors as internal before they are wrapped. GraphQL parse and validation errors are returned unchanged.

`post(id)` and `comment(id)` return `null` when the object does not exist. Comment length is counted in characters (`utf8.RuneCountInString`), not bytes.

**Identifiers.** Post and comment ids are `int64` (`BIGSERIAL`), which lets the id serve as the cursor. In a distributed system they could be replaced with UUIDv7, which is time-ordered and not enumerable. `authorId` is a string because users are external to the service.

## Tests

- Validation: empty fields, exactly 2000 and 2001 Cyrillic characters.
- Rules: missing post, disabled comments, parent from another post, settings changed by a non-author.
- Pagination walks all pages with no gaps or duplicates, and replies never appear among top-level comments.
- Batched reply loading.
- 100 concurrent comments get unique ids (`go test -race`).
- Subscriptions: delivery, isolation between posts, channel closed after disconnect.
- GraphQL layer, through the same server setup as production: error codes, masking of storage failures, cursor pagination, and the absence of N+1 (a three-level comment tree costs 3 storage calls instead of 16).

- Storage contract tests (`internal/storage/storagetest`): one suite that both the in-memory and the PostgreSQL storage must pass, covering ordering, cursors, per-parent limits in batched reply loading, and every comment rule.

The service is tested against the real in-memory storage with a fake broker.

## Assumptions

- There is no authentication: `authorId` is passed in the request. In a real system it would come from an auth token.
- The WebSocket endpoint accepts connections only from the same origin (default behavior); a separate frontend would require `OriginPatterns`.

## Possible improvements

- Subscriptions work within a single process. Multiple instances would need PostgreSQL `LISTEN/NOTIFY` or a message broker.
- A migration tool (e.g. goose) instead of applying the schema on startup.
- Run the PostgreSQL tests via testcontainers, so they need no manual setup.
- At Habr-level load: caching prebuilt comment trees for popular posts (as Reddit does) and a reply counter on comments.
