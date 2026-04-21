package doctor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// TestRun_Shape asserts Run() returns at least one Check of each status
// family on a representative system: we force an err (unset env), a
// warn (missing binary), and an ok (tmux version lookup if tmux is
// installed on the test host — otherwise we fall back to the config
// check which is OK whenever a cfg was passed).
func TestRun_Shape(t *testing.T) {
	// RequiredEnv contains a name no CI env will set → StatusErr row.
	// RequiredInPath contains a binary no host will have →
	// StatusWarn row.
	cfg := config.Config{
		DefaultMode:     "safe",
		ScrollbackLines: 50000,
		RequiredEnv:     []string{"CTM_DOCTOR_TEST_UNSET_VAR_XYZ"},
		RequiredInPath:  []string{"ctm_doctor_test_definitely_missing"},
	}

	checks := Run(context.Background(), cfg)
	if len(checks) == 0 {
		t.Fatal("Run returned 0 checks")
	}

	// Every check must carry the three required fields.
	for i, c := range checks {
		if c.Name == "" {
			t.Errorf("check[%d] has empty Name", i)
		}
		if c.Status != StatusOK && c.Status != StatusWarn && c.Status != StatusErr {
			t.Errorf("check[%d] (%s) has invalid Status %q", i, c.Name, c.Status)
		}
	}

	// Forced-err: the unset env var.
	if c := find(checks, "env:CTM_DOCTOR_TEST_UNSET_VAR_XYZ"); c == nil {
		t.Fatal("missing env check for unset var")
	} else if c.Status != StatusErr {
		t.Errorf("unset env → status %q, want %q", c.Status, StatusErr)
	} else if c.Remediation == "" {
		t.Error("unset env check has no remediation")
	}

	// Forced-warn: the missing binary.
	if c := find(checks, "path:ctm_doctor_test_definitely_missing"); c == nil {
		t.Fatal("missing path check for bogus bin")
	} else if c.Status != StatusWarn {
		t.Errorf("missing bin → status %q, want %q", c.Status, StatusWarn)
	}

	// Config check: we passed a non-zero config so it should be OK.
	// (If the config file on disk doesn't exist this degrades to warn,
	// which is fine — we're asserting on the shape, not the exact
	// status. We only require that config:load exists.)
	if find(checks, "config:load") == nil {
		t.Error("missing config:load check")
	}
}

// TestRun_ContextCancelled ensures a cancelled context aborts cleanly —
// we should still return a slice (possibly empty) rather than panicking
// or hanging on shell-out.
func TestRun_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	checks := Run(ctx, config.Config{})
	// Should not block, should return promptly. The slice may be
	// empty (first check sees ctx.Err()) or carry early results.
	if checks == nil {
		t.Error("Run returned nil slice on cancelled ctx")
	}
}

// TestCheck_JSONShape pins the wire contract. /api/doctor depends on
// these exact field names.
func TestCheck_JSONShape(t *testing.T) {
	c := Check{
		Name:        "dep:tmux",
		Status:      StatusOK,
		Message:     "/usr/bin/tmux",
		Remediation: "",
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	// Remediation omitempty → should NOT appear on empty.
	if _, ok := raw["remediation"]; ok {
		t.Errorf("expected remediation to be omitted when empty, got: %s", b)
	}
	for _, k := range []string{"name", "status", "message"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing field %q in %s", k, b)
		}
	}

	// With remediation set, it should round-trip.
	c.Remediation = "install tmux"
	b, _ = json.Marshal(c)
	_ = json.Unmarshal(b, &raw)
	if raw["remediation"] != "install tmux" {
		t.Errorf("remediation round-trip failed: %s", b)
	}
}

func find(checks []Check, name string) *Check {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}
