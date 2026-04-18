package jsonstrict

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecode_ValidClean_Succeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{"name":"alpha","count":3}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Name != "alpha" || got.Count != 3 {
		t.Errorf("got %+v, want {Name:alpha Count:3}", got)
	}

	// No backup should have been created.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.unknowns.") {
			t.Errorf("unexpected backup on clean path: %s", e.Name())
		}
	}
}

func TestDecode_UnknownField_StripsBacksUpAndSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"name":"alpha","count":3,"typo":"oops","extra":{"nested":true}}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err != nil {
		t.Fatalf("Decode expected to succeed after strip: %v", err)
	}
	if got.Name != "alpha" || got.Count != 3 {
		t.Errorf("got %+v, want {Name:alpha Count:3}", got)
	}

	// One backup must exist and contain the original bytes.
	entries, _ := os.ReadDir(dir)
	var backups []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.unknowns.") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 1 {
		t.Fatalf("want 1 backup, got %d: %v", len(backups), backups)
	}
	bdata, err := os.ReadFile(filepath.Join(dir, backups[0]))
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(bdata) != original {
		t.Errorf("backup diverges from original:\nwant: %s\ngot:  %s", original, bdata)
	}

	// Rewritten file must be strictly decodable (no unknowns).
	rewritten, _ := os.ReadFile(path)
	var check sample
	if err := strictUnmarshal(rewritten, &check); err != nil {
		t.Errorf("rewritten file still has unknowns: %v", err)
	}
	if strings.Contains(string(rewritten), `typo`) || strings.Contains(string(rewritten), `extra`) {
		t.Errorf("stripped keys still present in rewrite: %s", rewritten)
	}
}

func TestDecode_UnknownField_SecondCallIsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1,"typo":"oops"}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	// Second call on the already-cleaned file must not create another
	// backup — no unknowns left to strip.
	if err := Decode(path, &got); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var backups int
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.unknowns.") {
			backups++
		}
	}
	if backups != 1 {
		t.Errorf("run 2 created redundant backup: %d total", backups)
	}
}

func TestDecode_InvalidJSON_ErrorsFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{not json`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err == nil {
		t.Fatal("expected parse error, got nil")
	}

	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on parse error")
	}
}

func TestDecode_TypeMismatch_ErrorsFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"name":"alpha","count":"not-an-int"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err == nil {
		t.Fatal("expected type-mismatch error, got nil")
	}

	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on type error:\nwas: %s\nnow: %s", original, after)
	}
}

func TestDecode_MissingFile_ReturnsIsNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")

	var got sample
	err := Decode(path, &got)
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %v", err)
	}
}

func TestDecode_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1,"typo":"x"}`), 0640); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var got sample
	if err := Decode(path, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	info, _ := os.Stat(path)
	if mode := info.Mode().Perm(); mode != 0640 {
		t.Errorf("mode after strip = %v, want 0640 (preserved)", mode)
	}
}

func TestKnownJSONKeys_ExtractsTagsAndFallsBackToFieldName(t *testing.T) {
	type withUntagged struct {
		Tagged   string `json:"tagged_name"`
		Untagged string
		Ignored  string `json:"-"`
	}
	got := knownJSONKeys(&withUntagged{})
	if _, ok := got["tagged_name"]; !ok {
		t.Error("expected tagged_name in known set")
	}
	if _, ok := got["Untagged"]; !ok {
		t.Error("expected Untagged (Go name fallback) in known set")
	}
	if _, ok := got["Ignored"]; ok {
		t.Error("Ignored (json:\"-\") must not appear in known set")
	}
}

func TestKnownJSONKeys_AnonymousEmbeddedFlattens(t *testing.T) {
	type inner struct {
		Child string `json:"child"`
	}
	type outer struct {
		inner
		Top string `json:"top"`
	}
	got := knownJSONKeys(&outer{})
	for _, want := range []string{"child", "top"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected %q in known set", want)
		}
	}
}
