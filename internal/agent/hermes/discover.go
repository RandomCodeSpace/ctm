package hermes

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// discoverBudgetVar is the wall-clock ceiling for DiscoverSessionID.
// Hermes typically writes its session row within ~200ms of invocation;
// 5s gives plenty of slack. var (not const) so tests can shrink it.
var discoverBudgetVar = 5 * time.Second

// discoverPollVar is the interval between `hermes sessions list` calls.
// 250ms keeps latency tight without thrashing subprocesses.
var discoverPollVar = 250 * time.Millisecond

// sessionIDRe matches hermes' session ID at the trailing edge of a line:
//
//	YYYYMMDD_HHMMSS_<6+ hex chars>
//
// e.g. 20260515_152727_9da209. Anchored on end-of-line so we ignore
// stray hex elsewhere in the row.
var sessionIDRe = regexp.MustCompile(`(\d{8}_\d{6}_[0-9a-fA-F]{6,})\s*$`)

// hermesBin returns the hermes binary to invoke; honors CTM_HERMES_BIN.
func hermesBin() string {
	if b := os.Getenv("CTM_HERMES_BIN"); b != "" {
		return b
	}
	return "hermes"
}

// DiscoverSessionID polls `hermes sessions list --source cli --limit 10`
// for a row whose ID-encoded timestamp is at or after spawnStart, and
// returns the newest match.
//
// Returns ("", false) on timeout, missing binary, or any subprocess
// error — callers fall through to `hermes -c` semantics, which is
// strictly less precise but still correct.
func DiscoverSessionID(spawnStart time.Time) (string, bool) {
	deadline := time.Now().Add(discoverBudgetVar)
	cutoff := spawnStart.Truncate(time.Second)
	for {
		if id, ok := scanSessions(cutoff); ok {
			return id, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(discoverPollVar)
	}
}

// scanSessions runs `hermes sessions list --source cli --limit 10` and
// returns the newest session ID whose timestamp prefix is at or after
// cutoff. Returns ("", false) when no match is found, the subprocess
// fails, or the binary is missing.
func scanSessions(cutoff time.Time) (string, bool) {
	cmd := exec.Command(hermesBin(), "sessions", "list", "--source", "cli", "--limit", "10")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}

	var bestID string
	var bestTime time.Time
	for _, line := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		m := sessionIDRe.FindSubmatch(line)
		if m == nil {
			continue
		}
		id := string(m[1])
		// First 15 chars are "YYYYMMDD_HHMMSS"; the rest is the random suffix.
		t, err := time.ParseInLocation("20060102_150405", id[:15], time.Local)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			continue
		}
		if t.After(bestTime) {
			bestID = id
			bestTime = t
		}
	}
	if bestID == "" {
		return "", false
	}
	return bestID, true
}
