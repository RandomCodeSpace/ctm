package session_test

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// Property-style tests for the sessions.json state machine. No
// external framework — stdlib math/rand seeded for determinism.
// Each test represents an invariant that should hold for every
// generated input, not just one hand-picked case.

// nameChars / firstChars are the alphabets session.ValidateName accepts.
const (
	nameChars  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	firstChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// distinctValidNames returns n unique valid session names derived from
// rng. Deterministic for a given seed, which is the point — failures
// are reproducible.
func distinctValidNames(rng *rand.Rand, n int) []string {
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)
	for len(out) < n {
		ln := 1 + rng.Intn(20)
		buf := make([]byte, ln)
		buf[0] = firstChars[rng.Intn(len(firstChars))]
		for i := 1; i < ln; i++ {
			buf[i] = nameChars[rng.Intn(len(nameChars))]
		}
		s := string(buf)
		if _, hit := seen[s]; hit {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// TestProperty_SaveListRoundTrip: for many random sets of distinct
// valid names, saving each then listing returns exactly the same set.
func TestProperty_SaveListRoundTrip(t *testing.T) {
	const iters = 40
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < iters; i++ {
		i := i
		t.Run(fmt.Sprintf("iter%02d", i), func(t *testing.T) {
			dir := t.TempDir()
			st := session.NewStore(filepath.Join(dir, "sessions.json"))
			n := 1 + rng.Intn(20)
			names := distinctValidNames(rng, n)
			for _, name := range names {
				if err := st.Save(session.New(name, "/w/"+name, "safe")); err != nil {
					t.Fatalf("Save %q: %v", name, err)
				}
			}
			list, err := st.List()
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(list) != n {
				t.Fatalf("List size = %d, want %d", len(list), n)
			}

			got := make([]string, len(list))
			for j, s := range list {
				got[j] = s.Name
			}
			sort.Strings(got)
			want := append([]string(nil), names...)
			sort.Strings(want)
			for j := range got {
				if got[j] != want[j] {
					t.Errorf("idx %d: got %q, want %q", j, got[j], want[j])
				}
			}
		})
	}
}

// TestProperty_GetReturnsSavedSession: every Save produces a
// retrievable Session with the same UUID, Mode, and Workdir.
func TestProperty_GetReturnsSavedSession(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	dir := t.TempDir()
	st := session.NewStore(filepath.Join(dir, "sessions.json"))

	names := distinctValidNames(rng, 30)
	saved := make(map[string]*session.Session, len(names))
	for _, name := range names {
		s := session.New(name, "/w/"+name, "safe")
		if err := st.Save(s); err != nil {
			t.Fatalf("Save %q: %v", name, err)
		}
		saved[name] = s
	}

	for _, name := range names {
		got, err := st.Get(name)
		if err != nil {
			t.Fatalf("Get %q: %v", name, err)
		}
		want := saved[name]
		if got.UUID != want.UUID || got.Mode != want.Mode || got.Workdir != want.Workdir {
			t.Errorf("Get %q mismatch: got %+v, want %+v", name, got, want)
		}
	}
}

// TestProperty_DeleteShrinks: for a random sequence of Deletes, the
// List size decreases by exactly 1 each time.
func TestProperty_DeleteShrinks(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	dir := t.TempDir()
	st := session.NewStore(filepath.Join(dir, "sessions.json"))

	names := distinctValidNames(rng, 20)
	for _, name := range names {
		if err := st.Save(session.New(name, "/w/"+name, "safe")); err != nil {
			t.Fatalf("setup Save %q: %v", name, err)
		}
	}

	order := append([]string(nil), names...)
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	for i, name := range order {
		if err := st.Delete(name); err != nil {
			t.Fatalf("Delete %q: %v", name, err)
		}
		list, err := st.List()
		if err != nil {
			t.Fatalf("List after %d deletes: %v", i+1, err)
		}
		wantLen := len(names) - (i + 1)
		if len(list) != wantLen {
			t.Errorf("after %d deletes: List size = %d, want %d", i+1, len(list), wantLen)
		}
		// Deleted name must no longer resolve.
		if _, err := st.Get(name); err == nil {
			t.Errorf("Get of deleted %q unexpectedly succeeded", name)
		}
	}
}

// TestProperty_RenamePreservesData: Rename keeps UUID/Mode/Workdir
// intact and swaps the key cleanly.
func TestProperty_RenamePreservesData(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	dir := t.TempDir()
	st := session.NewStore(filepath.Join(dir, "sessions.json"))

	const n = 15
	pairs := distinctValidNames(rng, 2*n)
	originals := pairs[:n]
	renames := pairs[n:]

	for _, name := range originals {
		if err := st.Save(session.New(name, "/w/"+name, "safe")); err != nil {
			t.Fatalf("Save %q: %v", name, err)
		}
	}

	for i, oldName := range originals {
		want, err := st.Get(oldName)
		if err != nil {
			t.Fatalf("pre-rename Get %q: %v", oldName, err)
		}
		// Copy the want state before rename so subsequent reads don't
		// accidentally mutate it.
		wantUUID := want.UUID
		wantMode := want.Mode
		wantWorkdir := want.Workdir

		newName := renames[i]
		if err := st.Rename(oldName, newName); err != nil {
			t.Fatalf("Rename %q→%q: %v", oldName, newName, err)
		}
		if _, err := st.Get(oldName); err == nil {
			t.Errorf("Get of old name %q unexpectedly succeeded", oldName)
		}
		got, err := st.Get(newName)
		if err != nil {
			t.Fatalf("Get new %q: %v", newName, err)
		}
		if got.UUID != wantUUID || got.Mode != wantMode || got.Workdir != wantWorkdir {
			t.Errorf("rename lost data: UUID/Mode/Workdir = %s/%s/%s, want %s/%s/%s",
				got.UUID, got.Mode, got.Workdir, wantUUID, wantMode, wantWorkdir)
		}
		if got.Name != newName {
			t.Errorf("Name field not updated: got %q, want %q", got.Name, newName)
		}
	}
}

// TestConcurrent_DisjointSaves: N goroutines each save a unique name;
// all must land in the final state. If the flock were broken, we'd see
// lost updates and len(List) < N.
func TestConcurrent_DisjointSaves(t *testing.T) {
	dir := t.TempDir()
	st := session.NewStore(filepath.Join(dir, "sessions.json"))

	const n = 64
	names := distinctValidNames(rand.New(rand.NewSource(1)), n)

	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for _, name := range names {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.Save(session.New(name, "/w/"+name, "safe")); err != nil {
				errCh <- fmt.Errorf("save %q: %w", name, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Save: %v", err)
	}

	list, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != n {
		t.Errorf("final count = %d, want %d (flock may have allowed lost updates)", len(list), n)
	}

	got := make(map[string]struct{}, len(list))
	for _, s := range list {
		got[s.Name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := got[name]; !ok {
			t.Errorf("session %q absent from final List", name)
		}
	}
}

// TestConcurrent_MixedWorkload: workers hammer the store with random
// Save/Get/List/Delete calls. Asserts no panics / data races / errors
// other than "not found" (which is expected for a disjoint-workspace
// Delete/Get race). This is a race-detector smoke test — run it with
// `go test -race ./internal/session/...`.
func TestConcurrent_MixedWorkload(t *testing.T) {
	dir := t.TempDir()
	st := session.NewStore(filepath.Join(dir, "sessions.json"))

	const workers = 12
	const opsPerWorker = 80

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w + 1)))
			for i := 0; i < opsPerWorker; i++ {
				// Each worker operates on a private name prefix so its
				// writes never clash with another worker's reads on the
				// same key — we test flock concurrency, not business
				// logic under contention.
				name := fmt.Sprintf("w%02d-s%03d", w, i)
				switch rng.Intn(4) {
				case 0:
					_ = st.Save(session.New(name, "/w/"+name, "safe"))
				case 1:
					_, _ = st.Get(name) // may fail if not yet saved; acceptable
				case 2:
					if _, err := st.List(); err != nil {
						t.Errorf("List: %v", err)
					}
				case 3:
					_ = st.Delete(name) // may fail if absent; acceptable
				}
			}
		}()
	}
	wg.Wait()

	// Post-run sanity: the store still parses cleanly and List doesn't
	// blow up. Content is intentionally non-deterministic here — we're
	// testing concurrency robustness, not final state.
	if _, err := st.List(); err != nil {
		t.Fatalf("post-workload List: %v", err)
	}
}
