// Package jsonstrict decodes JSON files into typed structs with
// DisallowUnknownFields, with a one-time recovery path that strips
// unknown keys into a sibling backup on first encounter.
//
// This is the "breaking-change mitigation" described in
// docs/robustness-audit.md §5 for audit item #4. Strictness catches
// typos in hand-edited state files (config.json, sessions.json) from
// then on. On the first strict load that encounters unknowns, we do
// not hard-fail: we copy the original bytes to a sibling
// ".bak.unknowns.<unix-nano>", emit a WARN-level slog line naming the
// dropped keys, rewrite the file without them, and re-decode strictly.
// The mitigation is self-limiting — after the rewrite, subsequent
// loads see no unknowns and strict decode succeeds outright.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/fsutil"
)

// Decode reads path and strictly decodes into v (which must be a
// pointer to a struct). Behaviour:
//
//   - path missing → returns the underlying os.IsNotExist error so the
//     caller can decide what absent means in context. v is untouched.
//   - file valid + all keys known → v populated, nil returned.
//   - file valid + unknown keys → strip-to-.bak mitigation: original
//     bytes saved to "<path>.bak.unknowns.<unix-nano>", file rewritten
//     without the stripped keys, WARN slog line names each dropped key,
//     v populated with the cleaned result, nil returned.
//   - malformed JSON or type mismatch → error returned, file untouched,
//     v left in whatever partial state the decoder produced.
//
// The known-key set is derived from the `json` struct tags on v.
// Fields without a tag fall back to their Go name, matching how
// encoding/json's strict decoder lookups behave.
func Decode(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if err := strictUnmarshal(data, v); err == nil {
		return nil
	} else if !isUnknownFieldErr(err) {
		return err
	}

	// Recovery path: identify stripped keys, back up, rewrite, re-decode.
	var rawObj map[string]json.RawMessage
	if lerr := json.Unmarshal(data, &rawObj); lerr != nil {
		// Can't even lenient-parse; surface the strict error so the
		// caller sees the actual stopping point.
		return strictUnmarshal(data, v) //nolint:errcheck // returned below
	}

	known := knownJSONKeys(v)
	var stripped []string
	for k := range rawObj {
		if _, ok := known[k]; !ok {
			stripped = append(stripped, k)
			delete(rawObj, k)
		}
	}
	if len(stripped) == 0 {
		// Shouldn't happen given isUnknownFieldErr matched, but
		// defensive — return the strict error.
		return strictUnmarshal(data, v)
	}
	sort.Strings(stripped)

	info, _ := os.Stat(path)
	perm := os.FileMode(0600)
	if info != nil {
		perm = info.Mode().Perm()
	}
	backupPath := fmt.Sprintf("%s.bak.unknowns.%d", path, time.Now().UnixNano())
	if werr := os.WriteFile(backupPath, data, perm); werr != nil {
		return fmt.Errorf("jsonstrict backup before strip: %w", werr)
	}

	out, merr := json.MarshalIndent(rawObj, "", "  ")
	if merr != nil {
		return fmt.Errorf("jsonstrict marshal stripped: %w", merr)
	}

	if werr := fsutil.AtomicWriteFile(path, out, perm); werr != nil {
		return fmt.Errorf("jsonstrict write stripped: %w", werr)
	}

	slog.Warn("jsonstrict stripped unknown keys from state file",
		"path", path,
		"fields", stripped,
		"backup_path", backupPath)

	return strictUnmarshal(out, v)
}

func strictUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// isUnknownFieldErr matches the stdlib error format for unknown-field
// rejections. The concrete error type is unexported by encoding/json,
// so string matching is the canonical workaround.
func isUnknownFieldErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown field")
}

// knownJSONKeys enumerates the JSON-decodable keys of v via reflection
// on its struct tags. Anonymous (embedded) fields are flattened.
// Fields tagged `json:"-"` are excluded (they never round-trip through
// JSON). Untagged fields fall back to their Go field name, matching
// encoding/json's lookup behaviour.
func knownJSONKeys(v any) map[string]struct{} {
	known := map[string]struct{}{}
	collectKeys(reflect.TypeOf(v), known)
	return known
}

func collectKeys(t reflect.Type, out map[string]struct{}) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Anonymous (embedded) fields are flattened regardless of whether
		// the embedded type's own name is exported — their promoted
		// fields are what matter.
		if f.Anonymous {
			collectKeys(f.Type, out)
			continue
		}
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			name = f.Name
		}
		out[name] = struct{}{}
	}
}


var _ = strconv.Itoa // reserved for future use
