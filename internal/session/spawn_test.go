package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

type fakeTmux struct {
	newCalled int
	newArgs   struct{ name, workdir, shellCmd string }
	failNew   error
}

func (f *fakeTmux) NewSession(name, workdir, shellCmd string) error {
	f.newCalled++
	f.newArgs.name = name
	f.newArgs.workdir = workdir
	f.newArgs.shellCmd = shellCmd
	return f.failNew
}

func (f *fakeTmux) SendKeys(target, keys string) error {
	return nil
}

type fakeStore struct {
	saved *session.Session
	err   error
}

func (f *fakeStore) Save(s *session.Session) error {
	f.saved = s
	return f.err
}

func TestYolo_HappyPath(t *testing.T) {
	dir := t.TempDir()
	tmux := &fakeTmux{}
	store := &fakeStore{}

	got, err := session.Yolo(session.SpawnOpts{
		Name:    "smoke",
		Workdir: dir,
		Tmux:    tmux,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("Yolo: %v", err)
	}
	if got.Name != "smoke" || got.Mode != "yolo" || got.Workdir != dir {
		t.Fatalf("session fields = %+v", got)
	}
	if got.UUID == "" {
		t.Fatal("UUID should be generated")
	}
	if tmux.newCalled != 1 {
		t.Fatalf("NewSession called %d times, want 1", tmux.newCalled)
	}
	if tmux.newArgs.name != "smoke" || tmux.newArgs.workdir != dir {
		t.Fatalf("NewSession args = %+v", tmux.newArgs)
	}
	if store.saved == nil {
		t.Fatal("Store.Save not called")
	}
	if store.saved.UUID != got.UUID {
		t.Fatalf("stored UUID %q != returned %q", store.saved.UUID, got.UUID)
	}
}

func TestYolo_TmuxNewFails_DoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	tmux := &fakeTmux{failNew: errors.New("tmux exploded")}
	store := &fakeStore{}

	_, err := session.Yolo(session.SpawnOpts{
		Name: "x", Workdir: dir, Tmux: tmux, Store: store,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if store.saved != nil {
		t.Fatal("Store.Save should not be called when NewSession fails")
	}
}

func TestYolo_RejectsRelativeWorkdir(t *testing.T) {
	_, err := session.Yolo(session.SpawnOpts{
		Name: "x", Workdir: "relative/path",
		Tmux: &fakeTmux{}, Store: &fakeStore{},
	})
	if err == nil {
		t.Fatal("expected rejection of relative workdir")
	}
}

func TestYolo_RejectsMissingWorkdir(t *testing.T) {
	_, err := session.Yolo(session.SpawnOpts{
		Name: "x", Workdir: "/definitely/not/here/xyz/abc",
		Tmux: &fakeTmux{}, Store: &fakeStore{},
	})
	if err == nil {
		t.Fatal("expected rejection of missing workdir")
	}
}

func TestYolo_RejectsFileAsWorkdir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "notadir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := session.Yolo(session.SpawnOpts{
		Name: "x", Workdir: f,
		Tmux: &fakeTmux{}, Store: &fakeStore{},
	})
	if err == nil {
		t.Fatal("expected rejection of file-as-workdir")
	}
}
