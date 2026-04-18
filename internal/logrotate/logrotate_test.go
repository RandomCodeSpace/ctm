package logrotate

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func TestMaybeRotate_MissingFile_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")

	err := MaybeRotate(path, Policy{MaxSize: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("rotate created a file that didn't exist")
	}
}

func TestMaybeRotate_BelowThreshold_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, bytes.Repeat([]byte("a"), 50))

	if err := MaybeRotate(path, Policy{MaxSize: 1000}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	gzCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			gzCount++
		}
	}
	if gzCount != 0 {
		t.Errorf("rotation fired below threshold: %d .gz files", gzCount)
	}
}

func TestMaybeRotate_AboveThreshold_RotatesAndGzips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	payload := []byte("line-a\nline-b\nline-c\n")
	writeFile(t, path, payload)

	if err := MaybeRotate(path, Policy{MaxSize: 10}); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Active file must exist and be empty (fresh).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("active missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("active file not truncated; size=%d", info.Size())
	}

	// Exactly one rotated .gz sibling should exist.
	entries, _ := os.ReadDir(dir)
	var gzs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			gzs = append(gzs, e.Name())
		}
	}
	if len(gzs) != 1 {
		t.Fatalf("want 1 rotated .gz, got %d: %v", len(gzs), gzs)
	}

	// Rotated file must decompress to the original payload.
	f, err := os.Open(filepath.Join(dir, gzs[0]))
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gz: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("rotated contents diverge:\nwant: %q\ngot:  %q", payload, got)
	}
}

func TestMaybeRotate_PreservesActivePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, bytes.Repeat([]byte("a"), 200))

	if err := MaybeRotate(path, Policy{MaxSize: 10}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	info, _ := os.Stat(path)
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("post-rotation active mode = %v, want 0600", mode)
	}

	// The .gz should also be 0600.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".gz") {
			continue
		}
		info, _ := os.Stat(filepath.Join(dir, e.Name()))
		if mode := info.Mode().Perm(); mode != 0600 {
			t.Errorf("rotated .gz mode = %v, want 0600", mode)
		}
	}
}

func TestMaybeRotate_ZeroMaxSize_Disabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, bytes.Repeat([]byte("a"), 1<<20))

	if err := MaybeRotate(path, Policy{MaxSize: 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			t.Errorf("rotated despite MaxSize=0: %s", e.Name())
		}
	}
}

func TestPrune_AgeCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, []byte("active"))

	// Create three rotated siblings with distinct mtimes.
	old := filepath.Join(dir, "x.jsonl.1000000000000000000.gz")
	mid := filepath.Join(dir, "x.jsonl.2000000000000000000.gz")
	new_ := filepath.Join(dir, "x.jsonl.3000000000000000000.gz")
	for _, p := range []string{old, mid, new_} {
		writeFile(t, p, []byte("gz"))
	}
	now := time.Now()
	if err := os.Chtimes(old, now.Add(-40*24*time.Hour), now.Add(-40*24*time.Hour)); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(mid, now.Add(-20*24*time.Hour), now.Add(-20*24*time.Hour)); err != nil {
		t.Fatalf("chtimes mid: %v", err)
	}

	if err := Prune(path, Policy{MaxAge: 30 * 24 * time.Hour}); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old file not pruned despite exceeding MaxAge")
	}
	if _, err := os.Stat(mid); err != nil {
		t.Error("mid file incorrectly pruned (within MaxAge)")
	}
	if _, err := os.Stat(new_); err != nil {
		t.Error("new file incorrectly pruned")
	}
	// Active must not be touched.
	if _, err := os.Stat(path); err != nil {
		t.Error("active log pruned — it must never be touched by Prune")
	}
}

func TestPrune_MaxFilesCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, []byte("active"))

	// Create 5 rotated siblings; MaxFiles=2 → only the 2 newest survive.
	paths := []string{
		filepath.Join(dir, "x.jsonl.1.gz"),
		filepath.Join(dir, "x.jsonl.2.gz"),
		filepath.Join(dir, "x.jsonl.3.gz"),
		filepath.Join(dir, "x.jsonl.4.gz"),
		filepath.Join(dir, "x.jsonl.5.gz"),
	}
	base := time.Now().Add(-time.Hour)
	for i, p := range paths {
		writeFile(t, p, []byte("gz"))
		t := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, t, t); err != nil {
			panic(err)
		}
	}

	if err := Prune(path, Policy{MaxFiles: 2}); err != nil {
		t.Fatalf("prune: %v", err)
	}

	survived := 0
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			survived++
		}
	}
	if survived != 2 {
		t.Errorf("survivors = %d, want 2", survived)
	}
	// The two newest (index 3 and 4) must be present.
	for _, p := range []string{paths[3], paths[4]} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected newest survivor %s: %v", p, err)
		}
	}
}

func TestSources_ReturnsActiveAndRotatedInOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, []byte("active"))

	// Three rotated with increasing nano suffix = chronological order.
	r1 := filepath.Join(dir, "x.jsonl.1000.gz")
	r2 := filepath.Join(dir, "x.jsonl.2000.gz")
	r3 := filepath.Join(dir, "x.jsonl.3000.gz")
	for _, p := range []string{r1, r3, r2} {
		writeFile(t, p, []byte("gz"))
	}

	// Unrelated files must be ignored.
	writeFile(t, filepath.Join(dir, "other.jsonl"), []byte("x"))
	writeFile(t, filepath.Join(dir, "x.jsonl.bak"), []byte("x")) // not .gz

	got, err := Sources(path)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	want := []string{r1, r2, r3, path}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d: got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Sources[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOpen_HandlesGzipAndPlain(t *testing.T) {
	dir := t.TempDir()

	// Plain file.
	plain := filepath.Join(dir, "plain.jsonl")
	writeFile(t, plain, []byte(`{"a":1}`+"\n"))

	r, err := Open(plain)
	if err != nil {
		t.Fatalf("Open plain: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != `{"a":1}`+"\n" {
		t.Errorf("plain read: got %q", got)
	}

	// Gzipped file.
	gzPath := filepath.Join(dir, "x.jsonl.1.gz")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte(`{"b":2}` + "\n"))
	gw.Close()
	writeFile(t, gzPath, buf.Bytes())

	r, err = Open(gzPath)
	if err != nil {
		t.Fatalf("Open gz: %v", err)
	}
	got, _ = io.ReadAll(r)
	r.Close()
	if string(got) != `{"b":2}`+"\n" {
		t.Errorf("gz read: got %q", got)
	}
}
