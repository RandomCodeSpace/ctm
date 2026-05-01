package auth

import (
	"context"
	"testing"
)

func TestUserFrom_EmptyContext(t *testing.T) {
	if got := UserFrom(context.Background()); got != "" {
		t.Errorf("UserFrom(empty) = %q, want empty string", got)
	}
}

func TestWithUser_RoundTrip(t *testing.T) {
	ctx := WithUser(context.Background(), "alice@example.com")
	if got := UserFrom(ctx); got != "alice@example.com" {
		t.Errorf("UserFrom = %q, want alice@example.com", got)
	}
}

func TestWithUser_OverwritesPrevious(t *testing.T) {
	ctx := WithUser(context.Background(), "first")
	ctx = WithUser(ctx, "second")
	if got := UserFrom(ctx); got != "second" {
		t.Errorf("UserFrom = %q, want second", got)
	}
}

func TestUserFrom_ForeignKeyValueIgnored(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "shadow")
	if got := UserFrom(ctx); got != "" {
		t.Errorf("UserFrom(foreign key) = %q, want empty", got)
	}
}

func TestWithUser_PreservesParentValues(t *testing.T) {
	type parentKey struct{}
	parent := context.WithValue(context.Background(), parentKey{}, "kept")
	ctx := WithUser(parent, "bob")

	if got := UserFrom(ctx); got != "bob" {
		t.Errorf("UserFrom = %q, want bob", got)
	}
	if got, _ := ctx.Value(parentKey{}).(string); got != "kept" {
		t.Errorf("parent value = %q, want kept", got)
	}
}
