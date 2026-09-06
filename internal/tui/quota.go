package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/models"
	"github.com/mattn/go-runewidth"
)

type AccountResult struct {
	Name      string
	Usage     *models.Usage
	Error     string
	Note      string
	IsCurrent bool
}

type QuotaStyle struct {
	WarningThreshold int
	DangerThreshold  int
}

func DefaultQuotaStyle() QuotaStyle {
	return QuotaStyle{
		WarningThreshold: 50,
		DangerThreshold:  80,
	}
}

func FormatQuotaOverview(results []AccountResult, style QuotaStyle, currentAccount string) string {
	theme := NewTheme()
	width := GetTerminalWidth()

	if len(results) == 0 {
		return theme.Muted.Render("  " + i18n.T("tui.empty") + "\n")
	}

	for i := range results {
		if results[i].Name == currentAccount {
			results[i].IsCurrent = true
		}
	}

	if width < 60 {
		return formatCompact(results, style, theme)
	}
	return formatTable(results, style, theme, width)
}

func formatTable(results []AccountResult, style QuotaStyle, theme Theme, width int) string {
	var b strings.Builder

	nameWidth := computeNameWidth(results)

	usable := width - 2
	if usable < 40 {
		usable = 40
	}

	fixedTotal := nameWidth + 10
	availCols := usable - fixedTotal
	if availCols < 0 {
		availCols = 0
	}
	colWidth := availCols / 3
	if colWidth < 8 {
		colWidth = 8
	}

	// cell = bar + " " + pct("%3d%%"=4) + " " + reset(7 fixed)
	// so barWidth = colWidth - (1+4+1+7) = colWidth - 13
	barWidth := colWidth - 13
	if barWidth < 0 {
		barWidth = 0
	}

	b.WriteString(theme.Title.Render("  "+i18n.T("tui.title")+"  ") + theme.Muted.Render(i18n.T("tui.refreshed", time.Now().Format("15:04:05"))) + "\n\n")

	// 表头与内容对齐：行前有 2 格的当前账户标记（→ / 空格），表头的
	// 账户栏也要占同样的宽度，列标签一律左对齐才能与数据列对齐。
	header := "  " +
		lipgloss.PlaceHorizontal(nameWidth+2, lipgloss.Left, theme.Header.Render(i18n.T("tui.header.account"))) +
		"  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, theme.Header.Render(i18n.T("tui.header.5h"))) +
		"  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, theme.Header.Render(i18n.T("tui.header.weekly"))) +
		"  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, theme.Header.Render(i18n.T("tui.header.monthly"))) +
		"\n"
	b.WriteString(header)
	sepLen := nameWidth + 3*colWidth + 12
	if sepLen > usable {
		sepLen = usable
	}
	b.WriteString("  " + theme.Border.Render(strings.Repeat("─", sepLen)) + "\n")

	for _, result := range results {
		if result.Error != "" {
			b.WriteString(formatErrorRow(result, nameWidth, theme))
		} else {
			b.WriteString(formatQuotaRow(result, nameWidth, colWidth, barWidth, style, theme))
		}
	}

	b.WriteString("\n")
	summary := computeSummary(results, style)
	b.WriteString("  " + theme.Muted.Render(summary) + "\n")

	for _, r := range results {
		if r.IsCurrent {
			b.WriteString("  " + theme.Muted.Render(i18n.T("tui.active")) + theme.Active.Render(r.Name) + "\n")
			break
		}
	}

	best := findBestAccount(results)
	if best != "" {
		b.WriteString("  " + theme.Muted.Render(i18n.T("tui.best_available")) + theme.Success.Render(best) + "\n")
	}

	nextReset := findNextReset(results)
	if nextReset != "" {
		b.WriteString("  " + theme.Muted.Render(i18n.T("tui.next_reset")) + nextReset + "\n")
	}

	b.WriteString("\n  " + theme.Success.Render("●") + theme.Muted.Render(" "+i18n.T("tui.legend.healthy")+"  ") +
		theme.Warning.Render("▲") + theme.Muted.Render(" "+i18n.T("tui.legend.warning")+"  ") +
		theme.Danger.Render("●") + theme.Muted.Render(" "+i18n.T("tui.legend.critical")+"  ") +
		theme.Active.Render("→") + theme.Muted.Render(" "+i18n.T("tui.legend.active")) + "\n")

	return b.String()
}

func formatCompact(results []AccountResult, style QuotaStyle, theme Theme) string {
	var b strings.Builder
	b.WriteString("  " + i18n.T("tui.compact.title") + "\n\n")

	for _, result := range results {
		if result.Error != "" {
			marker := "  "
			if result.IsCurrent {
				marker = theme.Active.Render("→ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s  %s\n", marker, theme.Bold.Render(result.Name), theme.Error.Render(i18n.T("tui.error"))))
			b.WriteString(fmt.Sprintf("    %s\n", theme.Muted.Render(truncateError(result.Error, 40))))
		} else {
			marker := "  "
			if result.IsCurrent {
				marker = theme.Active.Render("→ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", marker, theme.Bold.Render(result.Name)))
			b.WriteString(fmt.Sprintf("    5H: %s  W: %s  M: %s\n",
				formatPercentCompact(result.Usage.Rolling, style, theme),
				formatPercentCompact(result.Usage.Weekly, style, theme),
				formatPercentCompact(result.Usage.Monthly, style, theme)))
			if result.Note != "" {
				b.WriteString(fmt.Sprintf("    %s\n", theme.Muted.Render(truncateError(result.Note, 60))))
			}
		}
	}

	summary := computeSummary(results, style)
	b.WriteString("\n  " + theme.Muted.Render(summary) + "\n")

	for _, r := range results {
		if r.IsCurrent {
			b.WriteString("  " + theme.Muted.Render(i18n.T("tui.active")) + theme.Active.Render(r.Name) + "\n")
			break
		}
	}

	best := findBestAccount(results)
	if best != "" {
		b.WriteString("\n  " + theme.Muted.Render(i18n.T("tui.best_available")) + theme.Success.Render(best) + "\n")
	}
	nextReset := findNextReset(results)
	if nextReset != "" {
		b.WriteString("  " + i18n.T("tui.reset") + nextReset + "\n")
	}
	return b.String()
}

func formatPercentCompact(window models.QuotaWindow, style QuotaStyle, theme Theme) string {
	if window.Status == "idle" {
		return theme.Muted.Render(i18n.T("tui.idle"))
	}
	if window.Status != "ok" {
		return theme.Muted.Render("n/a")
	}
	percent := window.Percent
	s := fmt.Sprintf("%d%%", percent)
	switch {
	case percent >= style.DangerThreshold:
		return theme.Danger.Render(s)
	case percent >= style.WarningThreshold:
		return theme.Warning.Render(s)
	default:
		return theme.Success.Render(s)
	}
}

func formatErrorRow(result AccountResult, nameWidth int, theme Theme) string {
	marker := "  "
	if result.IsCurrent {
		marker = theme.Active.Render("→ ")
	}
	name := theme.Bold.Render(padRight(result.Name, nameWidth))
	errText := theme.Error.Render("✗ " + truncateError(result.Error, 50))
	return fmt.Sprintf("  %s%s  %s\n", marker, name, errText)
}

func formatQuotaRow(result AccountResult, nameWidth, colWidth, barWidth int, style QuotaStyle, theme Theme) string {
	marker := "  "
	if result.IsCurrent {
		marker = theme.Active.Render("→ ")
	}

	name := theme.Account.Render(padRight(result.Name, nameWidth))
	rolling := formatQuotaCell(result.Usage.Rolling, barWidth, style, theme)
	weekly := formatQuotaCell(result.Usage.Weekly, barWidth, style, theme)
	monthly := formatQuotaCell(result.Usage.Monthly, barWidth, style, theme)

	row := "  " + marker + name + "  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, rolling) +
		"  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, weekly) +
		"  " +
		lipgloss.PlaceHorizontal(colWidth, lipgloss.Left, monthly)

	if result.Note != "" {
		row += "\n" + theme.Muted.Render(fmt.Sprintf("    ↳ %s", result.Note))
	}
	return row + "\n"
}

// windowStatus reports anything that is not a resolved "ok" window
// (unknown placeholders, empty status from providers without that window)
// as n/a instead of a misleading 0%. A dedicated "idle" status (the
// provider has no active usage window right now) gets its own label.
func formatQuotaCell(window models.QuotaWindow, barWidth int, style QuotaStyle, theme Theme) string {
	if window.Status == "idle" {
		return theme.Muted.Render(padRight("  "+i18n.T("tui.idle"), barWidth+13))
	}
	if window.Status != "ok" {
		return theme.Muted.Render(padRight("n/a", barWidth+13))
	}
	bar := renderBar(window.Percent, barWidth, style, theme)
	pct := formatPercent(window.Percent, style, theme)
	reset := theme.Muted.Render(" " + formatResetTimeFixed(window.ResetsAt))
	return bar + " " + pct + reset
}

// formatResetTimeFixed returns the reset time padded to a fixed display
// width so pct columns stay aligned across rows.
func formatResetTimeFixed(resetsAt time.Time) string {
	return padRight(formatResetTime(resetsAt), 7)
}

func renderBar(percent, width int, style QuotaStyle, theme Theme) string {
	filled := (percent * width) / 100
	// Provider data is unvalidated; clamp on both sides so a negative or
	// oversized percent can never reach strings.Repeat with a bad count.
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	barStyle := theme.Success
	switch {
	case percent >= style.DangerThreshold:
		barStyle = theme.Danger
	case percent >= style.WarningThreshold:
		barStyle = theme.Warning
	}

	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	return barStyle.Render(filledStr) + theme.BarEmpty.Render(emptyStr)
}

func formatPercent(percent int, style QuotaStyle, theme Theme) string {
	s := fmt.Sprintf("%3d%%", percent)
	switch {
	case percent >= style.DangerThreshold:
		return theme.Danger.Render(s)
	case percent >= style.WarningThreshold:
		return theme.Warning.Render(s)
	default:
		return theme.Success.Render(s)
	}
}

func formatResetTime(resetsAt time.Time) string {
	// 零值时间表示 API 没有返回该窗口的重置时间（如 Claude/Codex 没有 monthly 限制，
	// 或 5h 字段缺失）。显示 "n/a" 而不是误导性的 "expired"。
	if resetsAt.IsZero() {
		return "n/a"
	}
	duration := time.Until(resetsAt)
	if duration < 0 {
		return "expired"
	}

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes == 0 {
		return "<1m"
	}
	return fmt.Sprintf("%dm", minutes)
}

func computeNameWidth(results []AccountResult) int {
	width := 7
	for _, r := range results {
		w := runewidth.StringWidth(r.Name)
		if r.IsCurrent {
			w += 2
		}
		if w > width {
			width = w
		}
	}
	return width + 2
}

func padRight(s string, width int) string {
	sw := runewidth.StringWidth(s)
	if sw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-sw)
}

func computeSummary(results []AccountResult, style QuotaStyle) string {
	total := len(results)
	healthy := 0
	warnings := 0
	criticals := 0
	errors := 0
	unknowns := 0
	for _, r := range results {
		if r.Error != "" {
			errors++
			continue
		}
		maxPercent, hasData := maxKnownPercent(r.Usage)
		if !hasData {
			unknowns++
			continue
		}
		if maxPercent >= style.DangerThreshold {
			criticals++
		} else if maxPercent >= style.WarningThreshold {
			warnings++
		} else {
			healthy++
		}
	}

	parts := []string{}
	if healthy > 0 {
		parts = append(parts, i18n.T("tui.summary.healthy", healthy))
	}
	if warnings > 0 {
		parts = append(parts, i18n.T("tui.summary.warning", warnings))
	}
	if criticals > 0 {
		parts = append(parts, i18n.T("tui.summary.critical", criticals))
	}
	if unknowns > 0 {
		parts = append(parts, i18n.T("tui.summary.unknown", unknowns))
	}
	if errors > 0 {
		parts = append(parts, i18n.T("tui.summary.error", errors))
	}

	noun := i18n.T("tui.summary.accounts", total)
	if total == 1 {
		noun = i18n.T("tui.summary.account", total)
	}
	return fmt.Sprintf("%s  %s", noun, strings.Join(parts, "  "))
}

// maxKnownPercent returns the highest percentage across windows that carry
// real data. Windows with status != "ok" (unknown placeholders) are ignored;
// hasData is false when no window resolved.
func maxKnownPercent(u *models.Usage) (int, bool) {
	maxPercent := 0
	hasData := false
	for _, w := range []models.QuotaWindow{u.Rolling, u.Weekly, u.Monthly} {
		if w.Status != "ok" {
			continue
		}
		hasData = true
		if w.Percent > maxPercent {
			maxPercent = w.Percent
		}
	}
	return maxPercent, hasData
}

func findBestAccount(results []AccountResult) string {
	bestName := ""
	bestPercent := 101

	for _, r := range results {
		if r.Error != "" {
			continue
		}
		maxPercent, hasData := maxKnownPercent(r.Usage)
		if !hasData {
			continue // unknown usage cannot be compared
		}
		if maxPercent < bestPercent {
			bestPercent = maxPercent
			bestName = r.Name
		}
	}
	return bestName
}

func findNextReset(results []AccountResult) string {
	earliest := time.Time{}
	name := ""
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		for _, resetTime := range []time.Time{r.Usage.Rolling.ResetsAt, r.Usage.Weekly.ResetsAt, r.Usage.Monthly.ResetsAt} {
			if resetTime.IsZero() {
				continue
			}
			if earliest.IsZero() || resetTime.Before(earliest) {
				earliest = resetTime
				name = r.Name
			}
		}
	}
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s · %s", name, formatResetTime(earliest))
}

func truncateError(s string, maxLen int) string {
	w := runewidth.StringWidth(s)
	if w <= maxLen {
		return s
	}
	truncated := ""
	current := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if current+rw+1 > maxLen {
			truncated += "…"
			break
		}
		truncated += string(r)
		current += rw
	}
	return truncated
}
