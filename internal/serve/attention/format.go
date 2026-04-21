package attention

import (
	"fmt"
	"math"
)

func formatErrorBurst(errors, total int) string {
	return fmt.Sprintf("%d errors in last %d calls", errors, total)
}

func formatQuota(weekly float64, hasWeekly bool, fiveHr float64, hasFive bool) string {
	switch {
	case hasWeekly && hasFive:
		return fmt.Sprintf("weekly %d%% / 5h %d%%", roundPct(weekly), roundPct(fiveHr))
	case hasWeekly:
		return fmt.Sprintf("weekly %d%%", roundPct(weekly))
	case hasFive:
		return fmt.Sprintf("5h %d%%", roundPct(fiveHr))
	}
	return "quota threshold reached"
}

func formatCtx(pct int) string {
	return fmt.Sprintf("context window %d%%", pct)
}

func roundPct(p float64) int {
	return int(math.Round(p))
}
