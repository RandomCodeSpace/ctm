package health

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CheckEnvVars checks that all required environment variables are set.
// Returns fail with list of missing vars if any are absent.
func CheckEnvVars(required []string) CheckResult {
	var missing []string
	for _, key := range required {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return CheckResult{
			Name:    "env_vars",
			Status:  StatusFail,
			Message: fmt.Sprintf("missing environment variables: %s", strings.Join(missing, ", ")),
			Fix:     fmt.Sprintf("export %s=<value>", strings.Join(missing, " ")),
		}
	}
	return CheckResult{
		Name:    "env_vars",
		Status:  StatusPass,
		Message: "all required environment variables are set",
	}
}

// CheckPathEntries checks that all required binaries are available in PATH.
// An empty list always passes.
func CheckPathEntries(required []string) CheckResult {
	if len(required) == 0 {
		return CheckResult{
			Name:    "path_entries",
			Status:  StatusPass,
			Message: "no PATH entries required",
		}
	}

	var missing []string
	for _, bin := range required {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	if len(missing) > 0 {
		return CheckResult{
			Name:    "path_entries",
			Status:  StatusFail,
			Message: fmt.Sprintf("missing binaries in PATH: %s", strings.Join(missing, ", ")),
			Fix:     fmt.Sprintf("install or add to PATH: %s", strings.Join(missing, ", ")),
		}
	}
	return CheckResult{
		Name:    "path_entries",
		Status:  StatusPass,
		Message: "all required binaries found in PATH",
	}
}
