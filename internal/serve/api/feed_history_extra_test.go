package api

import (
	"strconv"
	"testing"
	"time"
)

// TestExtractTS_RFC3339 covers the RFC3339 (seconds-precision) branch.
func TestExtractTS_RFC3339(t *testing.T) {
	want := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	got := extractTS(map[string]any{
		"ctm_timestamp": want.Format(time.RFC3339),
	})
	if !got.Equal(want) {
		t.Errorf("extractTS RFC3339 = %v, want %v", got, want)
	}
}

// TestExtractTS_RFC3339Nano covers the nano-precision fallback branch.
func TestExtractTS_RFC3339Nano(t *testing.T) {
	want := time.Date(2026, 4, 21, 12, 0, 0, 123456789, time.UTC)
	got := extractTS(map[string]any{
		"ctm_timestamp": want.Format(time.RFC3339Nano),
	})
	if !got.Equal(want) {
		t.Errorf("extractTS RFC3339Nano = %v, want %v", got, want)
	}
}

// TestExtractTS_Missing covers the "no ctm_timestamp" → zero time branch.
func TestExtractTS_Missing(t *testing.T) {
	got := extractTS(map[string]any{"other_field": "abc"})
	if !got.IsZero() {
		t.Errorf("extractTS missing = %v, want zero", got)
	}
}

// TestExtractTS_WrongType covers the type-assertion-failure branch
// (ctm_timestamp present but not a string).
func TestExtractTS_WrongType(t *testing.T) {
	got := extractTS(map[string]any{"ctm_timestamp": 12345})
	if !got.IsZero() {
		t.Errorf("extractTS wrong-type = %v, want zero", got)
	}
}

// TestExtractTS_BadFormat covers the "string but unparseable" branch:
// neither RFC3339 nor RFC3339Nano accepts → zero time.
func TestExtractTS_BadFormat(t *testing.T) {
	got := extractTS(map[string]any{"ctm_timestamp": "not-a-timestamp"})
	if !got.IsZero() {
		t.Errorf("extractTS bad-format = %v, want zero", got)
	}
}

// TestNestedBool covers all branches of nestedBool: missing top key,
// non-map intermediate, missing leaf, leaf-not-bool, leaf-true.
func TestNestedBool(t *testing.T) {
	m := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"isit": true,
			},
			"scalar": "x",
		},
	}

	if !nestedBool(m, "a", "b", "isit") {
		t.Errorf("nestedBool(a.b.isit) = false, want true")
	}
	// missing top key
	if nestedBool(m, "missing", "x") {
		t.Errorf("nestedBool(missing.x) = true, want false")
	}
	// intermediate is not a map
	if nestedBool(m, "a", "scalar", "deeper") {
		t.Errorf("nestedBool(a.scalar.deeper) = true, want false")
	}
	// leaf missing
	if nestedBool(m, "a", "b", "missing") {
		t.Errorf("nestedBool(a.b.missing) = true, want false")
	}
	// leaf present but wrong type
	m2 := map[string]any{"flag": "true-as-string"}
	if nestedBool(m2, "flag") {
		t.Errorf("nestedBool wrong-type = true, want false")
	}
	// no path: returns whether root coerces to bool — root is map → false
	if nestedBool(m) {
		t.Errorf("nestedBool empty path on map = true, want false")
	}
}

// TestSummariseHistoryInput_NoToolInput exercises the "tool_input
// missing or wrong type" early return.
func TestSummariseHistoryInput_NoToolInput(t *testing.T) {
	if got := summariseHistoryInput(map[string]any{}, "Bash"); got != "" {
		t.Errorf("summariseHistoryInput no input = %q, want \"\"", got)
	}
	if got := summariseHistoryInput(map[string]any{"tool_input": "not-a-map"}, "Bash"); got != "" {
		t.Errorf("summariseHistoryInput wrong-type = %q, want \"\"", got)
	}
}

// TestSummariseHistoryInput_KnownToolPath returns the well-known
// primary input field via truncateToolInputField.
func TestSummariseHistoryInput_KnownToolPath(t *testing.T) {
	raw := map[string]any{
		"tool_input": map[string]any{
			"command": "echo hello",
		},
	}
	if got := summariseHistoryInput(raw, "Bash"); got != "echo hello" {
		t.Errorf("summariseHistoryInput Bash = %q, want \"echo hello\"", got)
	}
}

// TestSummariseHistoryInput_FallbackJSON exercises the json.Marshal
// fallback when the tool isn't well-known.
func TestSummariseHistoryInput_FallbackJSON(t *testing.T) {
	raw := map[string]any{
		"tool_input": map[string]any{
			"foo": "bar",
		},
	}
	got := summariseHistoryInput(raw, "UnknownTool")
	// Marshaled JSON should round-trip back something containing the key.
	if got == "" {
		t.Errorf("summariseHistoryInput fallback = \"\", want non-empty JSON")
	}
}

// TestSummariseHistoryResponse covers each switch arm of the response
// summariser: missing, string, map.output, map.is_error+error,
// map.is_error+no-error, map with arbitrary keys, empty map, and the
// "wrong type" default-fall-through.
func TestSummariseHistoryResponse(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		if got := summariseHistoryResponse(map[string]any{}); got != "" {
			t.Errorf("missing = %q, want \"\"", got)
		}
	})
	t.Run("string response", func(t *testing.T) {
		raw := map[string]any{"tool_response": "ok"}
		if got := summariseHistoryResponse(raw); got != "ok" {
			t.Errorf("string = %q, want \"ok\"", got)
		}
	})
	t.Run("string response truncated", func(t *testing.T) {
		long := make([]byte, historyInputMax+50)
		for i := range long {
			long[i] = 'x'
		}
		raw := map[string]any{"tool_response": string(long)}
		got := summariseHistoryResponse(raw)
		if len(got) == 0 || len(got) > historyInputMax {
			t.Errorf("string truncated len=%d, want <= %d and > 0", len(got), historyInputMax)
		}
	})
	t.Run("map output single line", func(t *testing.T) {
		raw := map[string]any{"tool_response": map[string]any{"output": "hello"}}
		if got := summariseHistoryResponse(raw); got != "hello" {
			t.Errorf("map.output single-line = %q, want \"hello\"", got)
		}
	})
	t.Run("map output multi-line takes first line", func(t *testing.T) {
		raw := map[string]any{"tool_response": map[string]any{"output": "first\nsecond\nthird"}}
		if got := summariseHistoryResponse(raw); got != "first" {
			t.Errorf("map.output multiline = %q, want \"first\"", got)
		}
	})
	t.Run("map is_error with message", func(t *testing.T) {
		raw := map[string]any{
			"tool_response": map[string]any{
				"is_error": true,
				"error":    "boom",
			},
		}
		if got := summariseHistoryResponse(raw); got != "boom" {
			t.Errorf("map is_error+error = %q, want \"boom\"", got)
		}
	})
	t.Run("map is_error with no message", func(t *testing.T) {
		raw := map[string]any{
			"tool_response": map[string]any{
				"is_error": true,
			},
		}
		if got := summariseHistoryResponse(raw); got != "error" {
			t.Errorf("map is_error+no-error = %q, want \"error\"", got)
		}
	})
	t.Run("map empty falls through to keys empty", func(t *testing.T) {
		raw := map[string]any{"tool_response": map[string]any{}}
		if got := summariseHistoryResponse(raw); got != "" {
			t.Errorf("empty map = %q, want \"\"", got)
		}
	})
	t.Run("map arbitrary keys → bracketed list", func(t *testing.T) {
		raw := map[string]any{
			"tool_response": map[string]any{
				"foo": "x",
				"bar": "y",
			},
		}
		got := summariseHistoryResponse(raw)
		// Map iteration order is random, but the wrapper format is
		// stable: starts with "[" and ends with "]".
		if len(got) < 2 || got[0] != '[' || got[len(got)-1] != ']' {
			t.Errorf("arbitrary keys = %q, want bracketed list", got)
		}
	})
	t.Run("unsupported response type", func(t *testing.T) {
		raw := map[string]any{"tool_response": 42}
		if got := summariseHistoryResponse(raw); got != "" {
			t.Errorf("unsupported = %q, want \"\"", got)
		}
	})
}

// TestTruncateHistory covers the trim-and-truncate helper directly.
func TestTruncateHistory(t *testing.T) {
	if got := truncateHistory("  hello  "); got != "hello" {
		t.Errorf("trim only = %q, want \"hello\"", got)
	}
	short := "abcdef"
	if got := truncateHistory(short); got != "abcdef" {
		t.Errorf("short pass-through = %q, want %q", got, short)
	}
	long := make([]byte, historyInputMax+10)
	for i := range long {
		long[i] = 'x'
	}
	got := truncateHistory(string(long))
	if len(got) != historyInputMax {
		t.Errorf("truncated len = %d, want %d", len(got), historyInputMax)
	}
}

// TestSplitIDExt and TestIDLessThanExt exercise the cursor-id parser
// and comparator end-to-end including malformed inputs.
func TestSplitIDExt(t *testing.T) {
	cases := []struct {
		id      string
		wantNS  int64
		wantSeq uint64
	}{
		{"1700000000-3", 1700000000, 3},
		{"42-0", 42, 0},
		{"", 0, 0},                   // no '-' → zeroes
		{"not-a-cursor", 0, 0},       // first segment unparseable, but '-' found
		{"123-notnum", 123, 0},       // seq unparseable
	}
	for _, c := range cases {
		ns, seq := splitIDExt(c.id)
		if ns != c.wantNS || seq != c.wantSeq {
			t.Errorf("splitIDExt(%q) = (%d, %d), want (%d, %d)",
				c.id, ns, seq, c.wantNS, c.wantSeq)
		}
	}
}

func TestIDLessThanExt(t *testing.T) {
	// older nano → less.
	if !idLessThanExt("100-0", "200-0") {
		t.Error("100-0 < 200-0 should be true")
	}
	// equal nano → seq decides.
	if !idLessThanExt("100-0", "100-1") {
		t.Error("100-0 < 100-1 should be true")
	}
	if idLessThanExt("200-0", "100-9") {
		t.Error("200-0 < 100-9 should be false")
	}
	// equal ids → not less.
	if idLessThanExt("100-1", "100-1") {
		t.Error("equal ids should not be less")
	}
}

// TestSynthEvent_BadJSON covers synthEvent's "json.Unmarshal failed"
// branch (returns ok=false).
func TestSynthEvent_BadJSON(t *testing.T) {
	if _, ok := synthEvent("alpha", []byte("not-json")); ok {
		t.Error("synthEvent should return false on invalid JSON")
	}
}

// TestSynthEvent_NoToolName covers the "tool_name missing" early return.
func TestSynthEvent_NoToolName(t *testing.T) {
	line := []byte(`{"foo":"bar"}`)
	if _, ok := synthEvent("alpha", line); ok {
		t.Error("synthEvent should return false when tool_name is missing")
	}
}

// TestSynthEvent_Happy verifies the synthesised envelope: id is
// derived from ctm_timestamp, type is tool_call, payload contains the
// session+tool.
func TestSynthEvent_Happy(t *testing.T) {
	ts := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	line := []byte(`{
		"tool_name":"Bash",
		"tool_input":{"command":"echo hi"},
		"tool_response":{"output":"hi","is_error":false},
		"ctm_timestamp":"` + ts.Format(time.RFC3339) + `"
	}`)
	ev, ok := synthEvent("alpha", line)
	if !ok {
		t.Fatal("synthEvent returned false on valid line")
	}
	if ev.Session != "alpha" {
		t.Errorf("Session = %q, want alpha", ev.Session)
	}
	if ev.Type != "tool_call" {
		t.Errorf("Type = %q, want tool_call", ev.Type)
	}
	wantID := strconv.FormatInt(ts.UnixNano(), 10) + "-0"
	if ev.ID != wantID {
		t.Errorf("ID = %q, want %q", ev.ID, wantID)
	}
}
