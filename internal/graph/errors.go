package graph

import (
	"context"
	"errors"
	"log/slog"

	"github.com/99designs/gqlgen/graphql"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func ErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	gqlErr := graphql.DefaultErrorPresenter(ctx, err)

	code := ""
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		code = "BAD_USER_INPUT"
	case errors.Is(err, service.ErrPostNotFound), errors.Is(err, service.ErrParentNotFound):
		code = "NOT_FOUND"
	case errors.Is(err, service.ErrCommentsDisabled), errors.Is(err, service.ErrForbidden):
		code = "FORBIDDEN"
	case errors.Is(err, service.ErrPostNotFound), errors.Is(err, service.ErrParentNotFound),
		errors.Is(err, service.ErrCommentNotFound):
		code = "NOT_FOUND"
	default:
		// Ошибки самого GraphQL (например, неверный тип аргумента) отдаём как есть.
		var e *gqlerror.Error
		if errors.As(err, &e) {
			return gqlErr
		}
		// Технические ошибки: подробности — в лог, клиенту — общее сообщение.
		slog.ErrorContext(ctx, "internal error", "err", err)
		gqlErr.Message = "internal error"
		code = "INTERNAL"
	}

	if gqlErr.Extensions == nil {
		gqlErr.Extensions = map[string]any{}
	}
	gqlErr.Extensions["code"] = code
	return gqlErr
}
