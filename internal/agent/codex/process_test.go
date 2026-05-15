package codex

import (
	"os"
	"strconv"
	"testing"
)

// process.go is a Linux-only /proc scanner. The tests here mirror the
// shape of internal/procscan/procscan_test.go but target the codex copy
// (which predates the shared procscan package and is still wired into
// some callers).

func TestIsCodexAlive_EmptyPID(t *testing.T) {
	if _, err := IsCodexAlive(""); err == nil {
		t.Fatal("expected error on empty pid, got nil")
	}
}

func TestIsCodexAlive_InvalidPID(t *testing.T) {
	// Same caveat as procscan: Sscanf accepts "12x" as 12, so the
	// branch we want is non-leading-digit + non-positive.
	for _, pid := range []string{"abc", "-1", "0", " "} {
		t.Run(pid, func(t *testing.T) {
			if _, err := IsCodexAlive(pid); err == nil {
				t.Fatalf("expected error for pid=%q", pid)
			}
		})
	}
}

func TestIsCodexAlive_NonexistentPID(t *testing.T) {
	alive, err := IsCodexAlive("2147483647")
	if err != nil {
		t.Fatalf("expected nil err for absent pid: %v", err)
	}
	if alive {
		t.Fatal("expected alive=false for absent pid")
	}
}

func TestIsCodexAlive_Self(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	alive, err := IsCodexAlive(pid)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !alive {
		t.Fatalf("expected alive=true for self pid %s", pid)
	}
}

func TestFindCodexChild_EmptyPID(t *testing.T) {
	if got := FindCodexChild(""); got != "" {
		t.Errorf("FindCodexChild(\"\") = %q, want \"\"", got)
	}
}

// TestFindCodexChild_NoMatch exercises the full /proc scan returning ""
// when no process matches the criteria. PPID "0" with no real codex
// child guarantees nothing matches in CI.
func TestFindCodexChild_NoMatch(t *testing.T) {
	if got := FindCodexChild("0"); got != "" {
		t.Errorf("FindCodexChild(0) = %q, want \"\"", got)
	}
}

func TestIsNumeric_Codex(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", true},
		{"12345", true},
		{"12a", false},
		{"-1", false},
		{"一", false},
	}
	for _, tc := range cases {
		if got := isNumeric(tc.in); got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReadProcStatus_Self(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	ppid, procName, ok := readProcStatus("/proc/" + pid + "/status")
	if !ok {
		t.Fatal("readProcStatus on self returned ok=false")
	}
	if ppid == "" || procName == "" {
		t.Errorf("expected non-empty fields, got ppid=%q procName=%q", ppid, procName)
	}
}

func TestReadProcStatus_MissingFile(t *testing.T) {
	_, _, ok := readProcStatus("/proc/2147483647/status")
	if ok {
		t.Error("expected ok=false for missing path")
	}
}
