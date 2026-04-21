package ingest

import "time"

// SetClockForTest replaces the projection's now() seam. Test-only:
// declared in an _test.go file so it is not part of the public API.
// Visible to external _test packages (e.g. ingest_test) so they can
// fast-forward the tmux liveness TTL without sleeping.
func SetClockForTest(p *Projection, now func() time.Time) {
	p.now = now
}
