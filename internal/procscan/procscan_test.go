package procscan

import (
	"os"
	"strconv"
	"testing"
)

// TestIsAlive_EmptyPID covers the leading-validation branch:
// an empty string must surface as an error, not (false, nil).
func TestIsAlive_EmptyPID(t *testing.T) {
	if _, err := IsAlive(""); err == nil {
		t.Fatal("expected error on empty pid, got nil")
	}
}

// TestIsAlive_InvalidPID covers the non-numeric / non-positive branch.
// (Sscanf("%d") is permissive about trailing junk, so "12x" parses to
// 12 — out of scope here. The branch we want is "no leading digit" and
// "<= 0".)
func TestIsAlive_InvalidPID(t *testing.T) {
	cases := []string{"abc", "-1", "0", " "}
	for _, pid := range cases {
		t.Run(pid, func(t *testing.T) {
			if _, err := IsAlive(pid); err == nil {
				t.Fatalf("expected error for pid=%q, got nil", pid)
			}
		})
	}
}

// TestIsAlive_NonexistentPID covers the "PID dir absent → (false, nil)"
// signal: a numerically-valid but unused PID is treated as dead, not an
// error. 2147483647 (max int32) is overwhelmingly likely to be free on
// any Linux box.
func TestIsAlive_NonexistentPID(t *testing.T) {
	alive, err := IsAlive("2147483647")
	if err != nil {
		t.Fatalf("expected nil err for absent pid, got: %v", err)
	}
	if alive {
		t.Fatal("expected alive=false for absent pid")
	}
}

// TestIsAlive_Self verifies the happy path: the running test process is
// alive, so IsAlive on os.Getpid() must return (true, nil).
func TestIsAlive_Self(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	alive, err := IsAlive(pid)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !alive {
		t.Fatalf("expected alive=true for self pid %s", pid)
	}
}

// TestFindChild_EmptyInputs covers the early-return guard: either
// argument empty yields "".
func TestFindChild_EmptyInputs(t *testing.T) {
	if got := FindChild("", "codex"); got != "" {
		t.Errorf("FindChild(\"\", codex) = %q, want \"\"", got)
	}
	if got := FindChild("1", ""); got != "" {
		t.Errorf("FindChild(1, \"\") = %q, want \"\"", got)
	}
}

// TestFindChild_NoMatch exercises the full /proc scan returning ""
// when no process has the given parent + comm. PPID 0 with a fake
// procName guarantees no real process matches.
func TestFindChild_NoMatch(t *testing.T) {
	if got := FindChild("0", "definitely-not-a-real-process-name-zzz"); got != "" {
		t.Errorf("FindChild with bogus comm = %q, want \"\"", got)
	}
}

// TestIsNumeric covers the helper directly. Empty, all-digit, mixed,
// and non-ASCII inputs.
func TestIsNumeric(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", true},
		{"12345", true},
		{"12a", false},
		{"a12", false},
		{"-1", false},
		{"1.0", false},
		{"一", false}, // non-ASCII rune
	}
	for _, tc := range cases {
		if got := isNumeric(tc.in); got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestReadStatus_Self confirms the parser reads /proc/<self>/status
// and returns a non-empty Name + PPid for the running process. Both
// fields are populated by the kernel for every live PID, so this
// exercises the success path without depending on synthetic fixtures.
func TestReadStatus_Self(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	ppid, comm, ok := readStatus("/proc/" + pid + "/status")
	if !ok {
		t.Fatal("readStatus on self returned ok=false")
	}
	if ppid == "" {
		t.Error("expected non-empty PPid")
	}
	if comm == "" {
		t.Error("expected non-empty Name")
	}
}

// TestReadStatus_MissingFile covers the os.Open error branch — a path
// that doesn't exist returns ("", "", false), not an error.
func TestReadStatus_MissingFile(t *testing.T) {
	ppid, comm, ok := readStatus("/proc/2147483647/status")
	if ok {
		t.Errorf("expected ok=false for missing path, got ppid=%q comm=%q", ppid, comm)
	}
}
