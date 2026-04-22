package auth

import "context"

type ctxKey struct{}

// WithUser returns a context carrying the authenticated username.
func WithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, ctxKey{}, user)
}

// UserFrom returns the authenticated username from ctx, or "" if
// there is none.
func UserFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}
