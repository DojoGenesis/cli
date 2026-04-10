// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ─── Panel Focus ────────────────────────────────────────────────────────────

// PanelFocus tracks which panel has keyboard focus for scrolling.
type PanelFocus int

const (
	FocusEventLog PanelFocus = iota
	FocusContext
	FocusStats
)

// ─── Panel Data Structs ─────────────────────────────────────────────────────

// PilotContext holds session metadata extracted from the SSE event stream.
type PilotContext struct {
	SessionID   string
	Specialist  string // from intent_classified events
	Disposition string
	Plugin      string
	Skills      []string
	Provider    string
	Model       string
	ProjectName string
}

// PilotStats holds aggregated counters for the stats panel.
type PilotStats struct {
	TotalEvents    int
	CoreEvents     int
	TraceEvents    int
	ArtifactEvents int
	OrchEvents     int
	ErrorCount     int
	TotalCostUSD   float64
	TotalTokensIn  int64
	TotalTokensOut int64
	Elapsed        time.Duration
}

// ─── Panel Styles ───────────────────────────────────────────────────────────

var (
	stylePanelBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorBorder))

	stylePanelBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorAmber))

	stylePanelTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAmber))

	stylePanelLabel = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray))

	stylePanelValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorWhite))
)

// ─── Render Functions ───────────────────────────────────────────────────────

// RenderEventPanel renders the main event log (left side of the layout).
// It displays a scrollable list of parsed events with color-coded severity.
func RenderEventPanel(events []ParsedEvent, scroll, width, height int, focused bool) string {
	// Account for border (2 chars each side) and title line.
	innerW := width - 2
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 2 // top + bottom border
	if innerH < 1 {
		innerH = 1
	}

	var sb strings.Builder

	// Compute visible slice.
	start := scroll
	end := start + innerH
	if end > len(events) {
		end = len(events)
	}

	var visible []ParsedEvent
	if start < len(events) {
		visible = events[start:end]
	}

	for _, e := range visible {
		ts := styleTimestamp.Render(e.Time)
		evType := styleEventType.Render(padRight(e.EventType, 16))

		// Truncate summary to fit panel width.
		summary := e.Summary
		maxData := innerW - 28
		if maxData < 10 {
			maxData = 10
		}
		if len(summary) > maxData {
			summary = summary[:maxData-1] + "…"
		}

		var styledSummary string
		switch e.Severity {
		case SeverityError:
			styledSummary = styleStatusErr.Render(summary)
		case SeverityWarning:
			styledSummary = styleCostYellow.Render(summary)
		case SeveritySuccess:
			styledSummary = styleStatusOK.Render(summary)
		default:
			styledSummary = styleDim.Render(summary)
		}
		sb.WriteString(fmt.Sprintf(" %s  %s  %s", ts, evType, styledSummary))
		sb.WriteString("\n")
	}

	// Pad remaining lines.
	for i := len(visible); i < innerH; i++ {
		sb.WriteString("\n")
	}

	content := strings.TrimRight(sb.String(), "\n")

	border := stylePanelBorder
	if focused {
		border = stylePanelBorderFocused
	}

	return border.
		Width(width - 2). // subtract border chars
		Height(innerH).
		Render(content)
}

// RenderContextPanel renders the context panel (top-right of the layout).
// Shows: active specialist, disposition, provider/model, project, session ID.
// When a DAG orchestration is active, the DAG node status is rendered below
// the session metadata.
func RenderContextPanel(ctx PilotContext, dag *DAGState, width, height int, focused bool) string {
	innerW := width - 2
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	var sb strings.Builder

	sb.WriteString(" " + stylePanelTitle.Render("Context") + "\n")

	lines := []struct{ label, value string }{
		{"Specialist", valueOrDash(ctx.Specialist)},
		{"Disposition", valueOrDash(ctx.Disposition)},
		{"Provider", valueOrDash(ctx.Provider)},
		{"Model", valueOrDash(ctx.Model)},
		{"Project", valueOrDash(ctx.ProjectName)},
		{"Plugin", valueOrDash(ctx.Plugin)},
		{"Session", truncStr(valueOrDash(ctx.SessionID), innerW-14)},
	}

	for _, l := range lines {
		label := stylePanelLabel.Render(padRight(l.label+":", 14))
		value := stylePanelValue.Render(l.value)
		sb.WriteString(" " + label + value + "\n")
	}

	// Skills list (if any).
	if len(ctx.Skills) > 0 {
		label := stylePanelLabel.Render(padRight("Skills:", 14))
		skillStr := strings.Join(ctx.Skills, ", ")
		if len(skillStr) > innerW-14 {
			skillStr = skillStr[:innerW-15] + "…"
		}
		sb.WriteString(" " + label + stylePanelValue.Render(skillStr) + "\n")
	}

	// DAG orchestration panel — rendered when an orchestration plan is active.
	if dag != nil && dag.Active {
		sb.WriteString("\n")
		// Budget remaining height for the DAG sub-panel.
		usedLines := 1 + len(lines) // title + metadata lines
		if len(ctx.Skills) > 0 {
			usedLines++
		}
		usedLines++ // blank separator line
		dagHeight := innerH - usedLines
		if dagHeight < 3 {
			dagHeight = 3
		}
		sb.WriteString(dag.Render(innerW, dagHeight))
	}

	// Pad remaining.
	content := strings.TrimRight(sb.String(), "\n")

	border := stylePanelBorder
	if focused {
		border = stylePanelBorderFocused
	}

	return border.
		Width(width - 2).
		Height(innerH).
		Render(content)
}

// RenderStatsPanel renders the stats panel (bottom-right of the layout).
// Shows: event counts by category, error count, cost, tokens, elapsed,
// and garden/memory context when loaded.
func RenderStatsPanel(stats PilotStats, garden GardenContext, width, height int, focused bool) string {
	innerW := width - 2
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	var sb strings.Builder

	sb.WriteString(" " + stylePanelTitle.Render("Stats") + "\n")

	// Event counts row.
	totalStr := stylePanelValue.Render(fmt.Sprintf("%d", stats.TotalEvents))
	var errStr string
	if stats.ErrorCount > 0 {
		errStr = styleStatusErr.Render(fmt.Sprintf(" (%d errors)", stats.ErrorCount))
	}
	sb.WriteString(" " + stylePanelLabel.Render(padRight("Events:", 14)) + totalStr + errStr + "\n")

	// Category breakdown.
	sb.WriteString(" " + stylePanelLabel.Render(padRight("  Core:", 14)) + styleDim.Render(fmt.Sprintf("%d", stats.CoreEvents)) + "\n")
	sb.WriteString(" " + stylePanelLabel.Render(padRight("  Trace:", 14)) + styleDim.Render(fmt.Sprintf("%d", stats.TraceEvents)) + "\n")
	sb.WriteString(" " + stylePanelLabel.Render(padRight("  Artifact:", 14)) + styleDim.Render(fmt.Sprintf("%d", stats.ArtifactEvents)) + "\n")
	sb.WriteString(" " + stylePanelLabel.Render(padRight("  Orch:", 14)) + styleDim.Render(fmt.Sprintf("%d", stats.OrchEvents)) + "\n")

	// Cost.
	costStr := fmt.Sprintf("$%.4f", stats.TotalCostUSD)
	var styledCost string
	switch {
	case stats.TotalCostUSD >= 1.0:
		styledCost = styleCostRed.Render(costStr)
	case stats.TotalCostUSD >= 0.10:
		styledCost = styleCostYellow.Render(costStr)
	default:
		styledCost = styleCostGreen.Render(costStr)
	}
	sb.WriteString(" " + stylePanelLabel.Render(padRight("Cost:", 14)) + styledCost + "\n")

	// Tokens.
	totalTokens := stats.TotalTokensIn + stats.TotalTokensOut
	tokenLine := formatTokens(totalTokens)
	if stats.TotalTokensIn > 0 || stats.TotalTokensOut > 0 {
		tokenLine += styleDim.Render(fmt.Sprintf(" (%s in / %s out)", formatTokens(stats.TotalTokensIn), formatTokens(stats.TotalTokensOut)))
	}
	sb.WriteString(" " + stylePanelLabel.Render(padRight("Tokens:", 14)) + stylePanelValue.Render(formatTokens(totalTokens)) + "\n")

	// Elapsed.
	elapsed := stats.Elapsed.Truncate(time.Second)
	sb.WriteString(" " + stylePanelLabel.Render(padRight("Elapsed:", 14)) + stylePanelValue.Render(elapsed.String()) + "\n")

	// ── Garden section (compact, 5-6 lines) ──
	if garden.Loaded {
		sb.WriteString("\n")
		sb.WriteString(" " + stylePanelTitle.Render("Garden") + "\n")
		sb.WriteString(" " + stylePanelLabel.Render(padRight("Seeds:", 14)) + stylePanelValue.Render(fmt.Sprintf("%d", garden.TotalSeeds)) + "\n")

		// Top 3 seeds by usage.
		top := 3
		if len(garden.TopSeeds) < top {
			top = len(garden.TopSeeds)
		}
		for i := 0; i < top; i++ {
			s := garden.TopSeeds[i]
			name := s.Name
			if len(name) > innerW-20 {
				name = name[:innerW-21] + "…"
			}
			sb.WriteString(" " + stylePanelLabel.Render(padRight(fmt.Sprintf("  #%d:", i+1), 14)) +
				styleDim.Render(fmt.Sprintf("%s (%d)", name, s.UsageCount)) + "\n")
		}

		// Last memory retrieval.
		if garden.LastRetrieval > 0 {
			sb.WriteString(" " + stylePanelLabel.Render(padRight("Retrieved:", 14)) +
				styleAccent.Render(fmt.Sprintf("%d memories", garden.LastRetrieval)) + "\n")
		}
	}

	_ = innerW // used implicitly via padRight widths
	_ = tokenLine

	content := strings.TrimRight(sb.String(), "\n")

	border := stylePanelBorder
	if focused {
		border = stylePanelBorderFocused
	}

	return border.
		Width(width - 2).
		Height(innerH).
		Render(content)
}

// ─── Panel Helpers ──────────────────────────────────────────────────────────

// valueOrDash returns the value if non-empty, otherwise "--".
func valueOrDash(s string) string {
	if s == "" {
		return "--"
	}
	return s
}

// truncStr truncates a string to maxLen, appending "…" if trimmed.
func truncStr(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}
