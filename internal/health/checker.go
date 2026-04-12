package health

const (
	StatusPass      = "pass"
	StatusFail      = "fail"
	StatusRecovered = "recovered"
)

// CheckResult holds the outcome of a single health check.
type CheckResult struct {
	Name    string
	Status  string
	Message string
	Fix     string
}

// Passed returns true if the check passed or was recovered.
func (r CheckResult) Passed() bool {
	return r.Status == StatusPass || r.Status == StatusRecovered
}

// CheckFunc is a function that performs a health check.
type CheckFunc func() CheckResult

// Runner executes a sequence of health checks.
type Runner struct {
	checks []CheckFunc
}

// NewRunner creates a new Runner with no checks registered.
func NewRunner() *Runner {
	return &Runner{}
}

// Add registers a check function to be run.
func (r *Runner) Add(fn CheckFunc) {
	r.checks = append(r.checks, fn)
}

// Results holds the outcomes of all executed checks.
type Results struct {
	Items []CheckResult
}

// AllPassed returns true if every check passed or recovered.
func (r Results) AllPassed() bool {
	for _, item := range r.Items {
		if !item.Passed() {
			return false
		}
	}
	return true
}

// Failures returns only the failed check results.
func (r Results) Failures() []CheckResult {
	var failures []CheckResult
	for _, item := range r.Items {
		if !item.Passed() {
			failures = append(failures, item)
		}
	}
	return failures
}

// Run executes all registered checks in order, stopping on the first failure.
func (r *Runner) Run() Results {
	var results Results
	for _, fn := range r.checks {
		result := fn()
		results.Items = append(results.Items, result)
		if !result.Passed() {
			break
		}
	}
	return results
}
