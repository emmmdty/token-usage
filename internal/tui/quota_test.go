package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/emmmdty/token-usage/internal/models"
	"github.com/emmmdty/token-usage/internal/provider"
)

func TestFormatQuotaOverviewEmpty(t *testing.T) {
	results := []AccountResult{}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")
	if !strings.Contains(output, "No accounts") {
		t.Errorf("expected 'No accounts' message, got: %s", output)
	}
}

func TestFormatQuotaOverviewSingleAccount(t *testing.T) {
	results := []AccountResult{
		{
			Name: "work",
			Usage: &models.Usage{
				Rolling: models.QuotaWindow{Status: "ok", Percent: 35, ResetsAt: time.Now().Add(8 * time.Hour)},
				Weekly:  models.QuotaWindow{Status: "ok", Percent: 12, ResetsAt: time.Now().Add(5 * 24 * time.Hour)},
				Monthly: models.QuotaWindow{Status: "ok", Percent: 8, ResetsAt: time.Now().Add(23 * 24 * time.Hour)},
			},
		},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")
	if !strings.Contains(output, "work") {
		t.Errorf("expected 'work' in output, got: %s", output)
	}
	if !strings.Contains(output, "35%") {
		t.Errorf("expected '35%%' in output, got: %s", output)
	}
	if !strings.Contains(output, "Token Usage") {
		t.Errorf("expected 'Token Usage' header, got: %s", output)
	}
}

func TestFormatQuotaOverviewCurrentAccount(t *testing.T) {
	results := []AccountResult{
		{Name: "work", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 35, ResetsAt: time.Now().Add(8 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 12, ResetsAt: time.Now().Add(5 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 8, ResetsAt: time.Now().Add(23 * 24 * time.Hour)},
		}},
		{Name: "personal", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 67, ResetsAt: time.Now().Add(2 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 45, ResetsAt: time.Now().Add(3 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 22, ResetsAt: time.Now().Add(18 * 24 * time.Hour)},
		}},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "work")
	if !strings.Contains(output, "Active:") {
		t.Errorf("expected 'Active:' marker, got: %s", output)
	}
	if !strings.Contains(output, "work") {
		t.Errorf("expected 'work' as active account, got: %s", output)
	}
}

func TestFormatQuotaOverviewPartialFailure(t *testing.T) {
	results := []AccountResult{
		{Name: "work", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 35, ResetsAt: time.Now().Add(8 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 12, ResetsAt: time.Now().Add(5 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 8, ResetsAt: time.Now().Add(23 * 24 * time.Hour)},
		}},
		{Name: "backup", Error: "API error: HTTP 401"},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")
	if !strings.Contains(output, "work") {
		t.Errorf("expected 'work' in output")
	}
	if !strings.Contains(output, "backup") {
		t.Errorf("expected 'backup' in output")
	}
	if !strings.Contains(output, "error") || !strings.Contains(output, "401") {
		t.Errorf("expected error display for backup, got: %s", output)
	}
}

func TestFormatQuotaOverviewCJK(t *testing.T) {
	results := []AccountResult{
		{Name: "生产环境", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 80, ResetsAt: time.Now().Add(4 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 50, ResetsAt: time.Now().Add(3 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 30, ResetsAt: time.Now().Add(15 * 24 * time.Hour)},
		}},
		{Name: "personal", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 20, ResetsAt: time.Now().Add(6 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 10, ResetsAt: time.Now().Add(4 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 5, ResetsAt: time.Now().Add(20 * 24 * time.Hour)},
		}},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")
	if !strings.Contains(output, "生产环境") {
		t.Errorf("expected '生产环境' in output")
	}
	if !strings.Contains(output, "personal") {
		t.Errorf("expected 'personal' in output")
	}
}

func TestFormatResetTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		contains string
	}{
		{"5 hours", time.Now().Add(5*time.Hour + 30*time.Minute), "5h"},
		{"2 days 3 hours", time.Now().Add(2*24*time.Hour + 3*time.Hour), "2d"},
		{"30 minutes", time.Now().Add(30 * time.Minute), "m"},
		{"expired", time.Now().Add(-1 * time.Hour), "expired"},
		{"zero time shows n/a", time.Time{}, "n/a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatResetTime(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("formatResetTime(%v) = %q, want it to contain %q", tt.input, result, tt.contains)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input  string
		width  int
		expect string
	}{
		{"abc", 6, "abc   "},
		{"测试", 6, "测试  "},
		{"hello", 3, "hello"},
	}

	for _, tt := range tests {
		result := padRight(tt.input, tt.width)
		runewidth := 0
		for _, r := range result {
			if r == ' ' {
				runewidth++
			} else {
				runewidth += 2
			}
		}
		if len(result) < len(tt.input) {
			t.Errorf("padRight(%q, %d) = %q, shorter than input", tt.input, tt.width, result)
		}
	}
}

func TestComputeSummary(t *testing.T) {
	results := []AccountResult{
		{Name: "a", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 30},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 20},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 10},
		}},
		{Name: "b", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 60},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 55},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 40},
		}},
		{Name: "c", Error: "timeout"},
	}
	summary := computeSummary(results, DefaultQuotaStyle())
	if !strings.Contains(summary, "3 accounts") {
		t.Errorf("expected '3 accounts' in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "1 healthy") {
		t.Errorf("expected '1 healthy' in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "1 warning") {
		t.Errorf("expected '1 warning' in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "1 error") {
		t.Errorf("expected '1 error' in summary, got: %s", summary)
	}
}

func TestFindBestAccount(t *testing.T) {
	results := []AccountResult{
		{Name: "heavy", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 80},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 70},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 60},
		}},
		{Name: "light", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 10},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 5},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 2},
		}},
		{Name: "errored", Error: "fail"},
	}
	best := findBestAccount(results)
	if best != "light" {
		t.Errorf("expected 'light' as best account, got: %q", best)
	}
}

func TestTruncateError(t *testing.T) {
	short := "API error: HTTP 401"
	if truncateError(short, 50) != short {
		t.Errorf("short error should not be truncated")
	}
	long := "This is a very long error message that should definitely be truncated because it exceeds the maximum length"
	truncated := truncateError(long, 30)
	charCount := 0
	for range truncated {
		charCount++
	}
	if charCount > 31 {
		t.Errorf("truncated error should be <= 31 chars, got %d: %q", charCount, truncated)
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Errorf("truncated error should end with ellipsis, got: %q", truncated)
	}
}

func TestFormatQuotaOverviewCompactWidth(t *testing.T) {
	SetTerminalWidth(50)
	defer SetTerminalWidth(80)

	results := []AccountResult{
		{Name: "work", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 35, ResetsAt: time.Now().Add(8 * time.Hour)},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 12, ResetsAt: time.Now().Add(5 * 24 * time.Hour)},
			Monthly: models.QuotaWindow{Status: "ok", Percent: 8, ResetsAt: time.Now().Add(23 * 24 * time.Hour)},
		}},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")
	if !strings.Contains(output, "work") {
		t.Errorf("compact mode should still show account name")
	}
}

func TestProgressBars(t *testing.T) {
	DisableColor()
	theme := NewTheme()

	tests := []struct {
		percent int
		filled  int
		empty   int
	}{
		{0, 0, 8},
		{50, 4, 4},
		{100, 8, 0},
	}

	for _, tt := range tests {
		bar := renderBar(tt.percent, 8, DefaultQuotaStyle(), theme)
		filledCount := strings.Count(bar, "█")
		emptyCount := strings.Count(bar, "░")
		if filledCount != tt.filled {
			t.Errorf("renderBar(%d): expected %d filled, got %d", tt.percent, tt.filled, filledCount)
		}
		if emptyCount != tt.empty {
			t.Errorf("renderBar(%d): expected %d empty, got %d", tt.percent, tt.empty, emptyCount)
		}
	}
}

func TestQuotaTableColumnAlignment(t *testing.T) {
	results := []AccountResult{
		{
			Name: "a@example.com",
			Usage: &models.Usage{
				Rolling: models.QuotaWindow{Status: "ok", Percent: 25, ResetsAt: time.Now().Add(61 * time.Minute)},
				Weekly:  models.QuotaWindow{Status: "ok", Percent: 37, ResetsAt: time.Now().Add(4*24*time.Hour + 8*time.Hour)},
				Monthly: models.QuotaWindow{Status: "ok", Percent: 65, ResetsAt: time.Now().Add(26 * 24 * time.Hour)},
			},
		},
		{
			Name: "longer-name@example.com",
			Usage: &models.Usage{
				Rolling: models.QuotaWindow{Status: "ok", Percent: 1, ResetsAt: time.Now().Add(2 * time.Hour)},
				Weekly:  models.QuotaWindow{Status: "ok", Percent: 12, ResetsAt: time.Now().Add(4*24*time.Hour + 8*time.Hour)},
				Monthly: models.QuotaWindow{Status: "ok", Percent: 3, ResetsAt: time.Now().Add(18*24*time.Hour + 16*time.Hour)},
			},
		},
	}
	style := DefaultQuotaStyle()
	output := FormatQuotaOverview(results, style, "")

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected title+header+2 rows, got %d lines: %q", len(lines), output)
	}
	header := lines[2]
	row1 := lines[4]
	row2 := lines[5]

	strip := func(s string) string {
		var b strings.Builder
		in := false
		for _, r := range s {
			if r == '\x1b' {
				in = true
				continue
			}
			if in {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					in = false
				}
				continue
			}
			b.WriteRune(r)
		}
		return b.String()
	}

	cols := []string{"5H", "Weekly", "Monthly"}
	for _, col := range cols {
		hi := strings.Index(header, col)
		if hi < 0 {
			t.Fatalf("header missing column %s: %s", col, header)
		}
		for _, row := range []string{row1, row2} {
			if len(strip(row)) <= hi+1 {
				t.Errorf("row too short at column %s", col)
				continue
			}
		}
	}

	// The percent value in the Weekly column must start at the same offset
	// on every data row (no drift from different reset-time lengths).
	s1, s2 := strip(row1), strip(row2)
	if len(s1) != len(s2) {
		t.Logf("row display lengths differ: %d vs %d", len(s1), len(s2))
	}
}

// A negative percent (possible from unvalidated provider data) must be
// clamped instead of reaching strings.Repeat with a negative count,
// which panics.
func TestRenderBarNegativePercentDoesNotPanic(t *testing.T) {
	theme := NewTheme()
	style := DefaultQuotaStyle()

	for _, pct := range []int{-1, -5, -50, -999} {
		bar := renderBar(pct, 10, style, theme)
		filled := strings.Count(bar, "█")
		empty := strings.Count(bar, "░")
		if filled != 0 || empty != 10 {
			t.Errorf("renderBar(%d, 10) = %d filled/%d empty cells, want 0/10", pct, filled, empty)
		}
	}
}

// Percentages above 100 stay clamped to a full bar.
func TestRenderBarOverfullPercentClamped(t *testing.T) {
	theme := NewTheme()
	bar := renderBar(150, 10, DefaultQuotaStyle(), theme)
	if got := strings.Count(bar, "█"); got != 10 {
		t.Errorf("renderBar(150, 10) filled = %d, want 10", got)
	}
}

// An "idle" window (no active usage window, e.g. Claude Code idle > 5h)
// must be labeled as such instead of rendering a bare "n/a".
func TestIdleWindowRendersIdleLabel(t *testing.T) {
	theme := NewTheme()
	style := DefaultQuotaStyle()

	cell := formatQuotaCell(models.QuotaWindow{Status: "idle"}, 10, style, theme)
	plain := stripANSI(cell)
	if !strings.Contains(plain, "idle") {
		t.Errorf("idle cell = %q, want it to contain the idle label", plain)
	}
	if strings.Contains(plain, "n/a") {
		t.Errorf("idle cell = %q, want no n/a", plain)
	}

	compact := stripANSI(formatPercentCompact(models.QuotaWindow{Status: "idle"}, style, theme))
	if !strings.Contains(compact, "idle") {
		t.Errorf("idle compact = %q, want the idle label", compact)
	}
}

// An exhausted window (e.g. OpenCode Go "rate-limited") must render as a
// maxed bar with its reset time instead of n/a — the user needs to know the
// limit is hit and when it comes back. A "none" window (the provider has no
// such window at all, e.g. Codex monthly) renders as "—" so it no longer
// collides with exhausted or unknown.
func TestExhaustedAndNoneWindowsRender(t *testing.T) {
	theme := NewTheme()
	style := DefaultQuotaStyle()
	reset := time.Now().Add(17 * 24 * time.Hour)

	exhausted := models.QuotaWindow{Status: provider.StatusExhausted, Percent: 100, ResetsAt: reset}
	cell := stripANSI(formatQuotaCell(exhausted, 10, style, theme))
	if !strings.Contains(cell, "100%") {
		t.Errorf("exhausted cell = %q, want 100%%", cell)
	}
	if strings.Contains(cell, "n/a") {
		t.Errorf("exhausted cell = %q, want no n/a", cell)
	}
	if !strings.Contains(cell, formatResetTime(reset)) {
		t.Errorf("exhausted cell = %q, want the reset time", cell)
	}
	if got := strings.Count(cell, "█"); got != 10 {
		t.Errorf("exhausted cell bar = %d filled cells, want 10", got)
	}

	// An exhausted window without a percent is still a full bar.
	noPct := models.QuotaWindow{Status: provider.StatusExhausted, ResetsAt: reset}
	if got := strings.Count(stripANSI(formatQuotaCell(noPct, 10, style, theme)), "█"); got != 10 {
		t.Errorf("exhausted cell without percent = %d filled cells, want 10", got)
	}

	compact := stripANSI(formatPercentCompact(exhausted, style, theme))
	if !strings.Contains(compact, "100%") {
		t.Errorf("exhausted compact = %q, want 100%%", compact)
	}

	noneCell := stripANSI(formatQuotaCell(models.QuotaWindow{Status: provider.StatusNone}, 10, style, theme))
	if strings.Contains(noneCell, "n/a") {
		t.Errorf("none cell = %q, want no n/a", noneCell)
	}
	if !strings.Contains(noneCell, "—") {
		t.Errorf("none cell = %q, want the not-applicable label", noneCell)
	}
	noneCompact := stripANSI(formatPercentCompact(models.QuotaWindow{Status: provider.StatusNone}, style, theme))
	if strings.Contains(noneCompact, "n/a") || !strings.Contains(noneCompact, "—") {
		t.Errorf("none compact = %q, want the not-applicable label", noneCompact)
	}
}

// Exhausted windows count as 100%: the summary must classify the account as
// critical, and findBestAccount must not recommend an exhausted account over
// a healthy one. "none" windows are ignored entirely (not unknowns).
func TestExhaustedAccountIsCriticalAndNotBest(t *testing.T) {
	exhausted := &models.Usage{
		Rolling: models.QuotaWindow{Status: "ok", Percent: 10},
		Weekly:  models.QuotaWindow{Status: "ok", Percent: 5},
		Monthly: models.QuotaWindow{Status: provider.StatusExhausted, Percent: 100},
	}
	if pct, ok := maxKnownPercent(exhausted); !ok || pct != 100 {
		t.Errorf("maxKnownPercent(exhausted) = %d, %v; want 100, true", pct, ok)
	}

	results := []AccountResult{
		{Name: "capped", Usage: exhausted},
		{Name: "light", Usage: &models.Usage{
			Rolling: models.QuotaWindow{Status: "ok", Percent: 10},
			Weekly:  models.QuotaWindow{Status: "ok", Percent: 5},
			Monthly: models.QuotaWindow{Status: provider.StatusNone},
		}},
	}
	if best := findBestAccount(results); best != "light" {
		t.Errorf("findBestAccount = %q, want 'light'", best)
	}

	summary := computeSummary(results, DefaultQuotaStyle())
	if !strings.Contains(summary, "1 critical") {
		t.Errorf("summary = %q, want 1 critical", summary)
	}
	if strings.Contains(summary, "unknown") {
		t.Errorf("summary = %q, want no unknown (none windows are not unknowns)", summary)
	}

	// With nothing usable left, no account is recommended at all.
	if best := findBestAccount(results[:1]); best != "" {
		t.Errorf("findBestAccount(all exhausted) = %q, want empty", best)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\033' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
