package claude

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/RandomCodeSpace/ctm/internal/fsutil"
)

// patchJSONFile reads path, applies patch to the top-level JSON object, and
// writes the result back atomically. Used to safely flip single keys inside
// JSON files owned by other tools (Claude Code's ~/.claude.json and
// ~/.claude/settings.json) without clobbering sibling keys.
//
// Contract:
//   - Missing file → no-op. Never creates it; the owning tool controls
//     lifecycle. Returns nil.
//   - Invalid JSON → returns a parse error without modifying the file.
//   - patch mutates the map in place and returns true to trigger a write,
//     false to skip. Returning false is a valid no-op.
//   - Writes are atomic (temp file in same dir + rename) and preserve the
//     original file mode.
//
// Concurrency caveat: if the owning tool writes to path between our Read and
// Rename, that write is lost. Acceptable when this runs before the
// competing writer launches (ctm bootstrap runs before `claude` starts).
//
// Key ordering: the map round-trip sorts keys alphabetically on marshal. The
// file's semantics are preserved (JSON parsers ignore order) but visual
// layout changes on the first write that mutates the object.
func patchJSONFile(path string, patch func(obj map[string]json.RawMessage) bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	if !patch(obj) {
		return nil
	}

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}

	return fsutil.AtomicWriteFile(path, out, info.Mode().Perm())
}
