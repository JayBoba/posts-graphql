package graph

import (
	"context"
	"errors"
	"log/slog"

	"github.com/99designs/gqlgen/graphql"
	"github.com/JayBoba/posts-graphql/internal/service"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// internalError marks a technical resolver error (e.g. from the database)
// whose details must not reach the client.
type internalError struct{ err error }

func (e internalError) Error() string { return e.err.Error() }
func (e internalError) Unwrap() error { return e.err }

// MarkInternalErrors is a field middleware. gqlgen wraps every resolver error in
// a *gqlerror.Error, so ErrorPresenter can no longer tell a database error from an
// argument parsing error. Here we see the error before it is wrapped: arguments are
// parsed before the resolver is called, so only resolver errors get here.
func MarkInternalErrors(ctx context.Context, next graphql.Resolver) (any, error) {
	res, err := next(ctx)
	if err == nil || errorCode(err) != "" {
		return res, err
	}
	var gqlErr *gqlerror.Error
	if errors.As(err, &gqlErr) {
		return res, err
	}
	return res, internalError{err}
}

func ErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	gqlErr := graphql.DefaultErrorPresenter(ctx, err)

	code := errorCode(err)
	if errors.As(err, new(internalError)) {
		// Technical errors: details go to the log, the client gets a generic message.
		slog.ErrorContext(ctx, "internal error", "err", err)
		gqlErr.Message = "internal error"
		code = "INTERNAL"
	}
	if code == "" {
		// GraphQL's own errors (parsing, validation, invalid arguments) are returned as is.
		return gqlErr
	}

	if gqlErr.Extensions == nil {
		gqlErr.Extensions = map[string]any{}
	}
	gqlErr.Extensions["code"] = code
	return gqlErr
}

// errorCode returns the code for the service's domain errors and "" for any other error.
func errorCode(err error) string {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		return "BAD_USER_INPUT"
	case errors.Is(err, service.ErrPostNotFound), errors.Is(err, service.ErrParentNotFound),
		errors.Is(err, service.ErrCommentNotFound):
		return "NOT_FOUND"
	case errors.Is(err, service.ErrCommentsDisabled), errors.Is(err, service.ErrForbidden):
		return "FORBIDDEN"
	default:
		return ""
	}
}
