package prompt

import (
	"strings"
	"testing"
)

func TestResolvePath_Dot(t *testing.T) {
	path, err := ResolvePath(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "." {
		t.Error("expected absolute path, got '.'")
	}
	if !strings.HasPrefix(path, "/") {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func TestResolvePath_Absolute(t *testing.T) {
	path, err := ResolvePath("/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/tmp" {
		t.Errorf("expected /tmp, got %q", path)
	}
}

func TestResolvePath_Home(t *testing.T) {
	path, err := ResolvePath("~/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(path, "/") {
		t.Errorf("expected absolute path starting with /, got %q", path)
	}
}

func TestResolvePath_Empty(t *testing.T) {
	_, err := ResolvePath("")
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
}

func TestResolvePath_NonExistent(t *testing.T) {
	_, err := ResolvePath("/tmp/ctm-nonexistent-dir-xyz-123")
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}
