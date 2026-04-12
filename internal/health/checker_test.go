package health

import (
	"testing"
)

func TestCheckResultPass(t *testing.T) {
	r := CheckResult{Name: "test", Status: StatusPass, Message: "ok"}
	if !r.Passed() {
		t.Errorf("expected StatusPass to be Passed(), got false")
	}
}

func TestCheckResultFail(t *testing.T) {
	r := CheckResult{Name: "test", Status: StatusFail, Message: "bad"}
	if r.Passed() {
		t.Errorf("expected StatusFail to not be Passed(), got true")
	}
}

func TestCheckResultRecovered(t *testing.T) {
	r := CheckResult{Name: "test", Status: StatusRecovered, Message: "fixed"}
	if !r.Passed() {
		t.Errorf("expected StatusRecovered to be Passed(), got false")
	}
}

func TestRunnerAllPass(t *testing.T) {
	runner := NewRunner()
	runner.Add(func() CheckResult {
		return CheckResult{Name: "check1", Status: StatusPass}
	})
	runner.Add(func() CheckResult {
		return CheckResult{Name: "check2", Status: StatusPass}
	})
	results := runner.Run()
	if !results.AllPassed() {
		t.Errorf("expected AllPassed() true, got false")
	}
	if len(results.Failures()) != 0 {
		t.Errorf("expected 0 failures, got %d", len(results.Failures()))
	}
}

func TestRunnerOneFails(t *testing.T) {
	runner := NewRunner()
	runner.Add(func() CheckResult {
		return CheckResult{Name: "check1", Status: StatusPass}
	})
	runner.Add(func() CheckResult {
		return CheckResult{Name: "check2", Status: StatusFail, Message: "broken"}
	})
	results := runner.Run()
	if results.AllPassed() {
		t.Errorf("expected AllPassed() false, got true")
	}
	failures := results.Failures()
	if len(failures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(failures))
	}
	if failures[0].Name != "check2" {
		t.Errorf("expected failure name check2, got %s", failures[0].Name)
	}
}
