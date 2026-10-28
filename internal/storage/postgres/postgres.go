package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JayBoba/posts-graphql/internal/domain"
)

//go:embed schema.sql
var schema string

const (
	postColumns    = "id, author_id, title, body, comments_enabled, created_at"
	commentColumns = "id, post_id, parent_id, author_id, body, created_at"
)

type Storage struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Storage{pool: pool}, nil
}

func (s *Storage) Close() { s.pool.Close() }

// Ping checks that the database is reachable; used by the health check.
func (s *Storage) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Storage) CreatePost(ctx context.Context, p domain.Post) (domain.Post, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO posts (author_id, title, body, comments_enabled)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+postColumns,
		p.AuthorID, p.Title, p.Body, p.CommentsEnabled)
	return scanPost(row)
}

func (s *Storage) GetPost(ctx context.Context, id int64) (domain.Post, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+postColumns+` FROM posts WHERE id = $1`, id)
	p, err := scanPost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Post{}, domain.ErrPostNotFound
	}
	return p, err
}

func (s *Storage) ListPosts(ctx context.Context, after *int64, limit int) ([]domain.Post, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+postColumns+` FROM posts
		 WHERE id < COALESCE($1::bigint, 9223372036854775807)
		 ORDER BY id DESC
		 LIMIT $2`,
		after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []domain.Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, rows.Err()
}

func (s *Storage) SetCommentsEnabled(ctx context.Context, postID int64, enabled bool) (domain.Post, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE posts SET comments_enabled = $2 WHERE id = $1 RETURNING `+postColumns,
		postID, enabled)
	p, err := scanPost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Post{}, domain.ErrPostNotFound
	}
	return p, err
}

func (s *Storage) CreateComment(ctx context.Context, c domain.Comment) (domain.Comment, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO comments (post_id, parent_id, author_id, body)
		 SELECT $1::bigint, $2::bigint, $3::text, $4::text
		 WHERE EXISTS (SELECT 1 FROM posts WHERE id = $1 AND comments_enabled)
		   AND ($2::bigint IS NULL OR EXISTS (
		         SELECT 1 FROM comments WHERE id = $2 AND post_id = $1))
		 RETURNING `+commentColumns,
		c.PostID, c.ParentID, c.AuthorID, c.Body)

	saved, err := scanComment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, s.whyNotInserted(ctx, c.PostID)
	}
	return saved, err
}

func (s *Storage) whyNotInserted(ctx context.Context, postID int64) error {
	post, err := s.GetPost(ctx, postID)
	switch {
	case err != nil:
		return err
	case !post.CommentsEnabled:
		return domain.ErrCommentsDisabled
	default:
		return domain.ErrParentNotFound
	}
}

func (s *Storage) GetComment(ctx context.Context, id int64) (domain.Comment, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = $1`, id)
	c, err := scanComment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, domain.ErrCommentNotFound
	}
	return c, err
}

func (s *Storage) ListComments(ctx context.Context, postID int64, parentID *int64, after *int64, limit int) ([]domain.Comment, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if parentID == nil {
		rows, err = s.pool.Query(ctx,
			`SELECT `+commentColumns+` FROM comments
			 WHERE post_id = $1 AND parent_id IS NULL AND id > COALESCE($2::bigint, 0)
			 ORDER BY id
			 LIMIT $3`,
			postID, after, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT `+commentColumns+` FROM comments
			 WHERE parent_id = $1 AND post_id = $2 AND id > COALESCE($3::bigint, 0)
			 ORDER BY id
			 LIMIT $4`,
			*parentID, postID, after, limit)
	}
	if err != nil {
		return nil, err
	}
	return collectComments(rows)
}

func scanPost(row pgx.Row) (domain.Post, error) {
	var p domain.Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.Title, &p.Body, &p.CommentsEnabled, &p.CreatedAt)
	return p, err
}

func scanComment(row pgx.Row) (domain.Comment, error) {
	var c domain.Comment
	err := row.Scan(&c.ID, &c.PostID, &c.ParentID, &c.AuthorID, &c.Body, &c.CreatedAt)
	return c, err
}

func collectComments(rows pgx.Rows) ([]domain.Comment, error) {
	defer rows.Close()
	var res []domain.Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}

func (s *Storage) ListRepliesBatch(ctx context.Context, parentIDs []int64, limit int) (map[int64][]domain.Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.post_id, c.parent_id, c.author_id, c.body, c.created_at
		 FROM unnest($1::bigint[]) AS p(pid)
		 CROSS JOIN LATERAL (
		     SELECT `+commentColumns+` FROM comments
		     WHERE parent_id = p.pid
		     ORDER BY id
		     LIMIT $2
		 ) AS c`,
		parentIDs, limit)
	if err != nil {
		return nil, err
	}
	comments, err := collectComments(rows)
	if err != nil {
		return nil, err
	}
	res := make(map[int64][]domain.Comment, len(parentIDs))
	for _, c := range comments {
		res[*c.ParentID] = append(res[*c.ParentID], c)
	}
	return res, nil
}
