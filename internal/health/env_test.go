package health

import (
	"testing"
)

func TestCheckEnvVars_AllPresent(t *testing.T) {
	result := CheckEnvVars([]string{"PATH", "HOME"})
	if !result.Passed() {
		t.Errorf("expected pass for PATH and HOME, got: %s — %s", result.Status, result.Message)
	}
}

func TestCheckEnvVars_Missing(t *testing.T) {
	result := CheckEnvVars([]string{"CTM_TOTALLY_FAKE_VAR_XYZ"})
	if result.Passed() {
		t.Errorf("expected fail for missing env var, got pass")
	}
	if result.Status != StatusFail {
		t.Errorf("expected StatusFail, got %s", result.Status)
	}
}

func TestCheckPathEntries_Empty(t *testing.T) {
	result := CheckPathEntries([]string{})
	if !result.Passed() {
		t.Errorf("expected pass for empty list, got: %s — %s", result.Status, result.Message)
	}
}

func TestCheckPathEntries_Missing(t *testing.T) {
	result := CheckPathEntries([]string{"ctm-totally-fake-binary-xyz"})
	if result.Passed() {
		t.Errorf("expected fail for missing binary, got pass")
	}
	if result.Status != StatusFail {
		t.Errorf("expected StatusFail, got %s", result.Status)
	}
}
