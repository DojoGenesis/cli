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
	"github.com/DojoGenesis/dojo-cli/internal/telemetry"
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

// gardenLoadedMsg delivers the garden context fetched at startup.
type gardenLoadedMsg GardenContext

// ─── Model ───────────────────────────────────────────────────────────────────

// PilotModel is the Bubbletea model for the live Pilot event-stream dashboard.
// It connects to the gateway's /events SSE endpoint and renders a multi-panel
// TUI with event log, context panel, and stats panel.
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

	// Telemetry sink — pushes SSE events to the D1 telemetry store.
	// nil when telemetry is disabled or unavailable.
	sink *telemetry.Sink

	// Multi-panel state
	focusPanel PanelFocus
	pilotCtx   PilotContext
	stats      PilotStats

	// DAG orchestration state — rendered in the context panel when active.
	dagState *DAGState

	// Garden/memory context — fetched at startup, updated by memory_retrieved events.
	garden GardenContext
}

// NewPilotModel constructs a PilotModel ready to be passed to tea.NewProgram.
// gw is an initialised gateway client; clientID is the SSE client identifier
// that will be appended to the /events URL as ?client_id=<clientID>.
func NewPilotModel(gw *client.Client, clientID string) PilotModel {
	ctx, cancel := context.WithCancel(context.Background())
	m := PilotModel{
		gw:          gw,
		clientID:    clientID,
		events:      make([]ParsedEvent, 0, 50),
		startTime:   time.Now(),
		ctx:         ctx,
		cancel:      cancel,
		costRateIn:  0.000003,  // default: sonnet input rate
		costRateOut: 0.000015,  // default: sonnet output rate
	}
	// Initialise the telemetry sink. It is always created so that events are
	// buffered; Start() launches the background flush goroutine.
	m.sink = telemetry.New(clientID)
	m.dagState = NewDAGState()
	return m
}

// Init starts the SSE listener goroutine, fetches garden context, and starts
// the telemetry sink, then returns the driving Cmds.
func (m PilotModel) Init() tea.Cmd {
	if m.sink != nil {
		m.sink.Start(m.ctx)
	}
	fetchGarden := func() tea.Msg {
		return gardenLoadedMsg(FetchGardenContext(m.gw))
	}
	return tea.Batch(m.listenSSE(), fetchGarden)
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
			if m.sink != nil {
				m.sink.Close()
			}
			m.cancel()
			return m, tea.Quit
		case "tab":
			// Cycle focus: EventLog -> Context -> Stats -> EventLog
			m.focusPanel = (m.focusPanel + 1) % 3
		case "shift+tab":
			// Reverse cycle
			if m.focusPanel == 0 {
				m.focusPanel = FocusStats
			} else {
				m.focusPanel--
			}
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

		// --- Stats: accumulate category counts ---
		m.stats.TotalEvents = m.count
		switch entry.Category {
		case CategoryCore:
			m.stats.CoreEvents++
		case CategoryTrace:
			m.stats.TraceEvents++
		case CategoryArtifact:
			m.stats.ArtifactEvents++
		case CategoryOrchestration:
			m.stats.OrchEvents++
		}
		if entry.Severity == SeverityError {
			m.stats.ErrorCount++
		}

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
				m.pilotCtx.Provider = p
			}
			if model, ok := entry.Parsed["model"].(string); ok {
				m.lastModel = model
				m.pilotCtx.Model = model
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

		// --- Context: map intent to specialist via client-side registry ---
		if entry.EventType == "intent_classified" {
			intent := getStr(entry.Parsed, "intent")
			confidence := getFloat(entry.Parsed, "confidence")
			spec := LookupSpecialist(intent, confidence)
			m.pilotCtx.Specialist = spec.Name
			m.pilotCtx.Disposition = spec.Disposition
			m.pilotCtx.Plugin = spec.Plugin
			m.pilotCtx.Skills = spec.Skills
			if sessionID, ok := entry.Parsed["session_id"].(string); ok {
				m.pilotCtx.SessionID = sessionID
			}
		}

		// --- Context: extract project name ---
		if entry.EventType == "project_switched" {
			if proj, ok := entry.Parsed["project_name"].(string); ok {
				m.pilotCtx.ProjectName = proj
			}
		}

		// --- Context: extract skills ---
		if entry.EventType == "tool_invoked" {
			if skill, ok := entry.Parsed["skill"].(string); ok && skill != "" {
				// Append if not already tracked (keep unique, max 5).
				found := false
				for _, s := range m.pilotCtx.Skills {
					if s == skill {
						found = true
						break
					}
				}
				if !found && len(m.pilotCtx.Skills) < 5 {
					m.pilotCtx.Skills = append(m.pilotCtx.Skills, skill)
				}
			}
		}

		// --- DAG: route orchestration events to the DAG state machine ---
		if strings.HasPrefix(entry.EventType, "orchestration_") && m.dagState != nil {
			m.dagState.HandleEvent(entry.EventType, entry.Parsed)
		}

		// --- Garden: update memory retrieval stats ---
		if entry.EventType == "memory_retrieved" {
			m.garden.HandleMemoryRetrieved(entry.Parsed)
		}

		// Sync cost/token stats.
		m.stats.TotalCostUSD = m.totalCostUSD
		m.stats.TotalTokensIn = m.totalTokensIn
		m.stats.TotalTokensOut = m.totalTokensOut
		m.stats.Elapsed = time.Since(m.startTime)

		// Push event to telemetry sink (best-effort, non-blocking).
		if m.sink != nil {
			m.sink.Ingest(entry.EventType, time.Now().UnixMilli(), entry.Parsed)
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

	case gardenLoadedMsg:
		m.garden = GardenContext(msg)
		return m, nil

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

// View renders the full multi-panel dashboard.
func (m PilotModel) View() string {
	if m.width == 0 {
		return "Connecting to Pilot stream...\n"
	}

	var sb strings.Builder

	// ── Header ──
	header := styleHeader.Render("  Pilot Dashboard")
	sessionLabel := styleSubtle.Render(fmt.Sprintf("  client: %s", m.clientID))
	sb.WriteString(header + "   " + sessionLabel + "\n")

	// ── Panel layout ──
	// Reserve: 1 header line + 1 status bar line = 2 lines overhead.
	panelHeight := m.height - 2
	if panelHeight < 4 {
		panelHeight = 4
	}

	// Left panel: 60% width. Right panel: 40% width.
	leftW := m.width * 60 / 100
	rightW := m.width - leftW
	if leftW < 20 {
		leftW = 20
	}
	if rightW < 16 {
		rightW = 16
	}

	// Right side is split vertically: context (top 40%), stats (bottom 60%).
	ctxH := panelHeight * 40 / 100
	if ctxH < 4 {
		ctxH = 4
	}
	statsH := panelHeight - ctxH
	if statsH < 4 {
		statsH = 4
	}

	// Sync elapsed time for stats display.
	statsSnap := m.stats
	statsSnap.Elapsed = time.Since(m.startTime)

	// Render each panel.
	eventPanel := RenderEventPanel(m.events, m.scroll, leftW, panelHeight, m.focusPanel == FocusEventLog)
	contextPanel := RenderContextPanel(m.pilotCtx, m.dagState, rightW, ctxH, m.focusPanel == FocusContext)
	statsPanel := RenderStatsPanel(statsSnap, m.garden, rightW, statsH, m.focusPanel == FocusStats)

	// Stack context + stats vertically on the right.
	rightSide := lipgloss.JoinVertical(lipgloss.Left, contextPanel, statsPanel)

	// Join left + right horizontally.
	panels := lipgloss.JoinHorizontal(lipgloss.Top, eventPanel, rightSide)
	sb.WriteString(panels + "\n")

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
	statusLeft := fmt.Sprintf(" %s %s", statusDot, connLabel)

	// Build cost segment for the right side.
	var costSegment string
	if m.totalTokensIn > 0 || m.totalTokensOut > 0 {
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
		tokenStr := styleDim.Render(formatTokens(totalTokens) + " tok")
		costSegment = styledCost + styleDim.Render(" | ") + tokenStr
	}

	// Focus indicator.
	var focusLabel string
	switch m.focusPanel {
	case FocusEventLog:
		focusLabel = "Events"
	case FocusContext:
		focusLabel = "Context"
	case FocusStats:
		focusLabel = "Stats"
	}

	helpKeys := styleSubtle.Render(fmt.Sprintf("Tab: switch | j/k: scroll | q: quit | %d events | %s | [%s]",
		m.count, elapsed, focusLabel))

	if costSegment != "" {
		gap := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(costSegment) - lipgloss.Width(helpKeys) - 4
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(statusLeft + styleDim.Render(" | ") + helpKeys + strings.Repeat(" ", gap) + costSegment)
	} else {
		gap := m.width - lipgloss.Width(statusLeft) - lipgloss.Width(helpKeys) - 2
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(statusLeft + styleDim.Render(" | ") + helpKeys + strings.Repeat(" ", gap))
	}

	return sb.String()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// visibleLines returns the number of event rows that fit in the event panel.
func (m PilotModel) visibleLines() int {
	// Panel height = total height - 2 (header + status bar) - 2 (panel border).
	n := m.height - 4
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
