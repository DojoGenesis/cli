// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DojoGenesis/dojo-cli/internal/client"
)

// ─── Dojo Genesis Color Palette ─────────────────────────────────────────────

const (
	colorAmber     = "#e8b04a" // warm amber — header / accents
	colorCloudGray = "#94a3b8" // cloud gray — timestamps / muted text
	colorInfoSteel = "#457b9d" // info steel — event types
	colorWhite     = "#f8fafc" // near-white — event data
	colorGreen     = "#22c55e" // connected indicator
	colorRed       = "#ef4444" // disconnected indicator
	colorBorder    = "#334155" // panel borders
	colorSubtle    = "#64748b" // secondary labels
	colorYellow    = "#eab308" // cost warning
)

// ─── Styles ──────────────────────────────────────────────────────────────────

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAmber))

	styleTimestamp = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray))

	styleEventType = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorInfoSteel)).
			Bold(true)

	styleEventData = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorWhite))

	styleStatusOK = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorGreen)).
			Bold(true)

	styleStatusErr = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorRed)).
			Bold(true)

	styleSubtle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle))

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorBorder))

	styleCostGreen = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorGreen)).
			Bold(true)

	styleCostYellow = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorYellow)).
			Bold(true)

	styleCostRed = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorRed)).
			Bold(true)

	styleAccent = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorInfoSteel))

	styleDim = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray))
)

// ─── Messages ────────────────────────────────────────────────────────────────

// sseEventMsg carries a single parsed SSE event to Bubbletea.
type sseEventMsg struct{ parsed ParsedEvent }

// sseErrorMsg carries a connection error to Bubbletea.
type sseErrorMsg struct{ err error }

// sseDoneMsg signals that the SSE stream has closed cleanly.
type sseDoneMsg struct{}

// ─── Model ───────────────────────────────────────────────────────────────────

// PilotModel is the Bubbletea model for the live Pilot event-stream dashboard.
// It connects to the gateway's /events SSE endpoint and renders a scrollable
// log of incoming events with connection status and elapsed-time tracking.
type PilotModel struct {
	gw        *client.Client
	clientID  string
	events    []ParsedEvent
	scroll    int
	width     int
	height    int
	connected bool
	count     int
	startTime time.Time
	err       error
	ctx       context.Context
	cancel    context.CancelFunc

	// Cost tracking (from "complete" events with usage data)
	totalTokensIn  int64
	totalTokensOut int64
	totalCostUSD   float64
	lastProvider   string
	lastModel      string
	costRateIn     float64 // per-token input rate (USD)
	costRateOut    float64 // per-token output rate (USD)
}

// NewPilotModel constructs a PilotModel ready to be passed to tea.NewProgram.
// gw is an initialised gateway client; clientID is the SSE client identifier
// that will be appended to the /events URL as ?client_id=<clientID>.
func NewPilotModel(gw *client.Client, clientID string) PilotModel {
	ctx, cancel := context.WithCancel(context.Background())
	return PilotModel{
		gw:          gw,
		clientID:    clientID,
		events:      make([]ParsedEvent, 0, 50),
		startTime:   time.Now(),
		ctx:         ctx,
		cancel:      cancel,
		costRateIn:  0.000003,  // default: sonnet input rate
		costRateOut: 0.000015,  // default: sonnet output rate
	}
}

// Init starts the SSE listener goroutine and returns its driving Cmd.
func (m PilotModel) Init() tea.Cmd {
	return m.listenSSE()
}

// listenSSE returns a Cmd that launches a goroutine to read the SSE stream.
// Each event is forwarded to Bubbletea as a sseEventMsg. Connection errors
// produce sseErrorMsg. A clean stream close produces sseDoneMsg.
func (m PilotModel) listenSSE() tea.Cmd {
	return func() tea.Msg {
		ch := make(chan tea.Msg, 1)
		go func() {
			err := m.gw.PilotStream(m.ctx, m.clientID, func(chunk client.SSEChunk) {
				ch <- sseEventMsg{parsed: ParseSSEEvent(chunk)}
			})
			if err != nil && m.ctx.Err() == nil {
				ch <- sseErrorMsg{err: err}
				return
			}
			ch <- sseDoneMsg{}
		}()
		return <-ch
	}
}

// waitForNext returns a Cmd that waits for the next message from the ongoing
// SSE goroutine. We use a persistent channel stored on the model.
func waitForNext(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// Update handles Bubbletea messages and produces the next model + Cmd.
func (m PilotModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			maxScroll := len(m.events) - m.visibleLines()
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.scroll < maxScroll {
				m.scroll++
			}
		}
		return m, nil

	case sseEventMsg:
		entry := msg.parsed
		m.events = append(m.events, entry)
		// Cap at last 50 events.
		if len(m.events) > 50 {
			m.events = m.events[len(m.events)-50:]
		}
		m.count++
		m.connected = true

		// --- Cost tracking: accumulate tokens from "complete" events ---
		if entry.EventType == "complete" {
			if usage, ok := entry.Parsed["usage"].(map[string]interface{}); ok {
				if tin, ok := usage["tokens_in"].(float64); ok {
					m.totalTokensIn += int64(tin)
					m.totalCostUSD += tin * m.costRateIn
				}
				if tout, ok := usage["tokens_out"].(float64); ok {
					m.totalTokensOut += int64(tout)
					m.totalCostUSD += tout * m.costRateOut
				}
			}
		}

		// --- Cost tracking: adjust rates on provider/model selection ---
		if entry.EventType == "provider_selected" {
			if p, ok := entry.Parsed["provider"].(string); ok {
				m.lastProvider = p
			}
			if model, ok := entry.Parsed["model"].(string); ok {
				m.lastModel = model
				lower := strings.ToLower(model)
				switch {
				case strings.Contains(lower, "haiku"):
					m.costRateIn = 0.0000008
					m.costRateOut = 0.000004
				case strings.Contains(lower, "opus"):
					m.costRateIn = 0.000015
					m.costRateOut = 0.000075
				default: // sonnet and anything else
					m.costRateIn = 0.000003
					m.costRateOut = 0.000015
				}
			}
		}

		// Auto-scroll to bottom when user is at (or near) bottom.
		maxScroll := len(m.events) - m.visibleLines()
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.scroll >= maxScroll-1 {
			m.scroll = maxScroll
		}
		// Schedule the next read.
		return m, m.listenSSE()

	case sseErrorMsg:
		m.connected = false
		m.err = msg.err
		return m, nil

	case sseDoneMsg:
		m.connected = false
		return m, nil
	}

	return m, nil
}

// View renders the full dashboard.
func (m PilotModel) View() string {
	if m.width == 0 {
		return "Connecting to Pilot stream…\n"
	}

	var sb strings.Builder

	// ── Header ──
	header := styleHeader.Render("  Pilot Dashboard")
	sessionLabel := styleSubtle.Render(fmt.Sprintf("  client: %s", m.clientID))
	sb.WriteString(header + "   " + sessionLabel + "\n")
	sb.WriteString(styleSubtle.Render(strings.Repeat("─", m.width)) + "\n")

	// ── Event log ──
	logHeight := m.height - 5 // reserve 2 header + 1 sep + 2 status lines
	if logHeight < 1 {
		logHeight = 1
	}

	start := m.scroll
	end := start + logHeight
	if end > len(m.events) {
		end = len(m.events)
	}

	visible := m.events
	if start < len(visible) {
		visible = visible[start:end]
	} else {
		visible = nil
	}

	for _, e := range visible {
		ts := styleTimestamp.Render(e.Time)
		evType := styleEventType.Render(padRight(e.EventType, 16))
		// Truncate summary to terminal width minus fixed prefix width (28 chars).
		summary := e.Summary
		maxData := m.width - 28
		if maxData < 10 {
			maxData = 10
		}
		if len(summary) > maxData {
			summary = summary[:maxData-1] + "…"
		}
		// Color-code by severity.
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
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n", ts, evType, styledSummary))
	}

	// Pad remaining lines so the status bar stays at the bottom.
	for i := len(visible); i < logHeight; i++ {
		sb.WriteString("\n")
	}

	// ── Separator ──
	sb.WriteString(styleSubtle.Render(strings.Repeat("─", m.width)) + "\n")

	// ── Status bar ──
	var statusDot string
	if m.connected {
		statusDot = styleStatusOK.Render("●")
	} else {
		statusDot = styleStatusErr.Render("●")
	}

	var connLabel string
	if m.err != nil {
		connLabel = styleStatusErr.Render(fmt.Sprintf("error: %s", m.err.Error()))
	} else if m.connected {
		connLabel = styleStatusOK.Render("streaming")
	} else {
		connLabel = styleSubtle.Render("disconnected")
	}

	elapsed := time.Since(m.startTime).Truncate(time.Second)
	statusLeft := fmt.Sprintf("  %s  %s", statusDot, connLabel)
	statusMid := styleSubtle.Render(fmt.Sprintf("%d events | %s", m.count, elapsed))

	// Build cost segment for the right side.
	var costSegment string
	if m.totalTokensIn > 0 || m.totalTokensOut > 0 {
		// Cost string with color based on spend.
		costStr := fmt.Sprintf("$%.4f", m.totalCostUSD)
		var styledCost string
		switch {
		case m.totalCostUSD >= 1.0:
			styledCost = styleCostRed.Render(costStr)
		case m.totalCostUSD >= 0.10:
			styledCost = styleCostYellow.Render(costStr)
		default:
			styledCost = styleCostGreen.Render(costStr)
		}

		totalTokens := m.totalTokensIn + m.totalTokensOut
		tokenStr := styleDim.Render(formatTokens(totalTokens) + " tokens")

		// Provider/model label (truncated if too long).
		var modelLabel string
		if m.lastProvider != "" && m.lastModel != "" {
			ml := m.lastProvider + "/" + m.lastModel
			if len(ml) > 24 {
				ml = ml[:23] + "…"
			}
			modelLabel = styleAccent.Render(ml)
		}

		if modelLabel != "" {
			costSegment = styledCost + styleDim.Render(" | ") + tokenStr + styleDim.Render(" | ") + modelLabel
		} else {
			costSegment = styledCost + styleDim.Render(" | ") + tokenStr
		}
	}

	// Compose the full status line.
	if costSegment != "" {
		// Three-part layout: left | mid ... cost right
		leftAndMid := statusLeft + styleDim.Render(" | ") + statusMid
		gap := m.width - lipgloss.Width(leftAndMid) - lipgloss.Width(costSegment) - 1
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(leftAndMid + strings.Repeat(" ", gap) + costSegment + "\n")
	} else {
		// No cost data yet — original layout.
		statusRight := styleSubtle.Render(fmt.Sprintf("events: %d   elapsed: %s   q/esc quit", m.count, elapsed))
		gap := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(statusRight)
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(statusLeft + strings.Repeat(" ", gap) + statusRight + "\n")
	}

	return sb.String()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// visibleLines returns the number of event rows that fit in the current terminal.
func (m PilotModel) visibleLines() int {
	n := m.height - 5
	if n < 1 {
		return 1
	}
	return n
}

// padRight pads s to width w with spaces (or truncates if longer).
func padRight(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

// formatTokens returns a human-readable token count with K/M suffix.
func formatTokens(n int64) string {
	if n >= 1_000_000 {
		v := float64(n) / 1_000_000
		// Use 1 decimal place, strip trailing zero.
		s := fmt.Sprintf("%.1f", math.Round(v*10)/10)
		s = strings.TrimSuffix(s, ".0")
		return s + "M"
	}
	if n >= 1_000 {
		v := float64(n) / 1_000
		s := fmt.Sprintf("%.1f", math.Round(v*10)/10)
		s = strings.TrimSuffix(s, ".0")
		return s + "K"
	}
	return fmt.Sprintf("%d", n)
}
