package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAliasBlock(t *testing.T) {
	block := AliasBlock()
	checks := []string{
		"alias ct='ctm'",
		"alias ctl='ctm ls'",
		"alias cty='ctm yolo'",
		startMarker,
		endMarker,
	}
	for _, want := range checks {
		if !strings.Contains(block, want) {
			t.Errorf("AliasBlock() missing %q", want)
		}
	}
}

func TestInjectAliases_NewFile(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")

	if err := InjectAliases(rc); err != nil {
		t.Fatalf("InjectAliases: %v", err)
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, startMarker) {
		t.Error("expected start marker in new file")
	}
	if !strings.Contains(content, "alias ct='ctm'") {
		t.Error("expected alias in new file")
	}
}

func TestInjectAliases_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")

	if err := InjectAliases(rc); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := InjectAliases(rc); err != nil {
		t.Fatalf("second inject: %v", err)
	}

	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	count := strings.Count(content, startMarker)
	if count != 1 {
		t.Errorf("expected 1 alias block, got %d", count)
	}
}

func TestRemoveAliases(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")

	if err := InjectAliases(rc); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := RemoveAliases(rc); err != nil {
		t.Fatalf("remove: %v", err)
	}

	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "ctm") {
		t.Errorf("expected no ctm aliases after removal, got: %q", content)
	}
	if strings.Contains(content, startMarker) {
		t.Error("expected no start marker after removal")
	}
}
