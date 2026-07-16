// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── War Room Constants ─────────────────────────────────────────────────────

const (
	scoutSuffix      = "You are a strategic scout. Explore possibilities, find routes through the problem, and synthesize options. Be thorough and measured."
	challengerSuffix = "You are a professional challenger. Find the strongest objection to whatever was just proposed. Do not hedge. Lead with the objection. If you cannot find a genuine flaw, say so explicitly."
	maxPanelLines    = 500
)

// ─── War Room Styles ────────────────────────────────────────────────────────

var (
	// Active panel border (amber highlight)
	wrScoutBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorAmber)).
				Padding(0, 1)

	wrChallengerBorderActive = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color(colorAmber)).
					Padding(0, 1)

	// Inactive panel border (dim)
	wrScoutBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorInfoSteel)).
			Padding(0, 1)

	wrChallengerBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorRed)).
				Padding(0, 1)

	wrInputBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorAmber)).
			Padding(0, 1)

	wrInputBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorGreen)).
				Padding(0, 1)

	wrTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorAmber))

	wrScoutLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorInfoSteel))

	wrChallengerLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(colorRed))

	wrInputPrompt = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAmber))

	wrStatusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle))

	wrStreamingDot = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorGreen))

	wrScrollHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle))
)

// ─── War Room Messages ──────────────────────────────────────────────────────

type scoutChunkMsg string
type challengerChunkMsg string
type scoutDoneMsg struct{}
type challengerDoneMsg struct{}
type scoutErrorMsg struct{ err error }
type challengerErrorMsg struct{ err error }

// autoDispatchMsg triggers an immediate dispatch of the pre-filled topic.
type autoDispatchMsg struct{ text string }

// ─── War Room chat request ──────────────────────────────────────────────────

// warRoomChatRequest is the request body for the war room's gateway calls.
type warRoomChatRequest struct {
	Message            string `json:"message"`
	Model              string `json:"model,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Stream             bool   `json:"stream"`
	SessionID          string `json:"session_id"`
	SystemPromptSuffix string `json:"system_prompt_suffix,omitempty"`
	Disposition        string `json:"disposition,omitempty"`
}

// ─── War Room Model ─────────────────────────────────────────────────────────

// focusPanel tracks which panel has scroll focus.
type focusPanel int

const (
	focusInput      focusPanel = iota
	focusScout
	focusChallenger
)

// WarRoomModel is the Bubbletea model for the split-panel debate TUI.
type WarRoomModel struct {
	// Input
	inputBuf  []rune
	cursorPos int

	// Two agent panels
	scoutBuf           *strings.Builder
	challengerBuf      *strings.Builder
	scoutLines         []string
	challengerLines    []string
	scoutScroll        int
	challengerScroll   int
	scoutStreaming     bool
	challengerStreaming bool

	// Streaming channels — nil when no active stream.
	// Each channel delivers tea.Msgs from one agent goroutine.
	// The Update loop calls waitForWarRoomMsg after every chunk to keep the
	// delivery chain alive, exactly like pilot.go's listenSSE/waitForNext pattern.
	scoutCh      <-chan tea.Msg
	challengerCh <-chan tea.Msg

	// Focus
	focus focusPanel

	// Layout
	width  int
	height int

	// Gateway connection
	gatewayURL   string
	gatewayToken string
	model        string
	provider     string
	sessionID    string

	// Context — m.cancel() stops all activity on quit.
	ctx    context.Context
	cancel context.CancelFunc

	// Initial topic to pre-fill (and auto-dispatch on Init).
	initialTopic string

	// Error state
	err error //nolint:unused // pending: top-level last-error field for a model-wide error banner; per-panel errors currently render inline via scoutBuf/challengerBuf instead
}

// NewWarRoomModel constructs a WarRoomModel ready for tea.NewProgram.
// If initialTopic is non-empty it is pre-filled in the input and auto-dispatched
// immediately so the debate starts without requiring the user to press Enter.
func NewWarRoomModel(gatewayURL, gatewayToken, model, provider, sessionID, initialTopic string) WarRoomModel {
	ctx, cancel := context.WithCancel(context.Background())
	m := WarRoomModel{
		inputBuf:      make([]rune, 0, 256),
		scoutBuf:      &strings.Builder{},
		challengerBuf: &strings.Builder{},
		focus:         focusInput,
		gatewayURL:    gatewayURL,
		gatewayToken:  gatewayToken,
		model:         model,
		provider:      provider,
		sessionID:     sessionID,
		ctx:           ctx,
		cancel:        cancel,
		initialTopic:  initialTopic,
	}
	if initialTopic != "" {
		m.inputBuf = []rune(initialTopic)
		m.cursorPos = len(m.inputBuf)
	}
	return m
}

// Init auto-dispatches the initial topic when one was provided.
func (m WarRoomModel) Init() tea.Cmd {
	if m.initialTopic != "" {
		topic := m.initialTopic
		return func() tea.Msg { return autoDispatchMsg{text: topic} }
	}
	return nil
}

// ─── Update ─────────────────────────────────────────────────────────────────

func (m WarRoomModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case autoDispatchMsg:
		return m.dispatch(msg.text)

	// ── Scout stream ──────────────────────────────────────────────────────

	case scoutChunkMsg:
		m.scoutBuf.WriteString(string(msg))
		m.scoutLines = wrapText(m.scoutBuf.String(), m.panelContentWidth())
		m.pinScoutBottom()
		// Keep the delivery chain alive — wait for next message on the channel.
		return m, waitForWarRoomMsg(m.scoutCh)

	case scoutDoneMsg:
		m.scoutStreaming = false
		m.scoutCh = nil
		return m, nil

	case scoutErrorMsg:
		m.scoutStreaming = false
		m.scoutCh = nil
		fmt.Fprintf(m.scoutBuf, "\n[error: %v]", msg.err)
		m.scoutLines = wrapText(m.scoutBuf.String(), m.panelContentWidth())
		return m, nil

	// ── Challenger stream ─────────────────────────────────────────────────

	case challengerChunkMsg:
		m.challengerBuf.WriteString(string(msg))
		m.challengerLines = wrapText(m.challengerBuf.String(), m.panelContentWidth())
		m.pinChallengerBottom()
		return m, waitForWarRoomMsg(m.challengerCh)

	case challengerDoneMsg:
		m.challengerStreaming = false
		m.challengerCh = nil
		return m, nil

	case challengerErrorMsg:
		m.challengerStreaming = false
		m.challengerCh = nil
		fmt.Fprintf(m.challengerBuf, "\n[error: %v]", msg.err)
		m.challengerLines = wrapText(m.challengerBuf.String(), m.panelContentWidth())
		return m, nil
	}

	return m, nil
}

// pinScoutBottom pins the scout scroll to the bottom of the content.
func (m *WarRoomModel) pinScoutBottom() {
	vis := m.panelViewHeight()
	if max := len(m.scoutLines) - vis; max > 0 {
		m.scoutScroll = max
	} else {
		m.scoutScroll = 0
	}
}

// pinChallengerBottom pins the challenger scroll to the bottom of the content.
func (m *WarRoomModel) pinChallengerBottom() {
	vis := m.panelViewHeight()
	if max := len(m.challengerLines) - vis; max > 0 {
		m.challengerScroll = max
	} else {
		m.challengerScroll = 0
	}
}

// dispatch clears the panels and starts both agent streams for the given text.
// It is safe to call from Update (handleKey Enter) and from autoDispatchMsg.
// Returns early if either stream is still active (prevents channel overlap).
func (m WarRoomModel) dispatch(text string) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}

	// Don't allow a new dispatch while agents are still streaming.
	if m.scoutStreaming || m.challengerStreaming {
		return m, nil
	}

	// Clear input and previous responses.
	m.inputBuf = m.inputBuf[:0]
	m.cursorPos = 0
	m.scoutBuf.Reset()
	m.challengerBuf.Reset()
	m.scoutLines = nil
	m.challengerLines = nil
	m.scoutScroll = 0
	m.challengerScroll = 0
	m.scoutStreaming = true
	m.challengerStreaming = true

	// Create fresh channels for this dispatch.
	scoutCh := make(chan tea.Msg, 128)
	challengerCh := make(chan tea.Msg, 128)
	m.scoutCh = scoutCh
	m.challengerCh = challengerCh

	// Launch goroutines — they own and close the channels.
	go streamAgentToChannel(
		m.ctx, m.gatewayURL, m.gatewayToken, m.model, m.provider,
		m.sessionID+"-measured", text, scoutSuffix, "measured", true, scoutCh,
	)
	go streamAgentToChannel(
		m.ctx, m.gatewayURL, m.gatewayToken, m.model, m.provider,
		m.sessionID+"-adversarial", text, challengerSuffix, "adversarial", false, challengerCh,
	)

	// Seed the delivery chains — each Cmd blocks until the next message arrives.
	return m, tea.Batch(waitForWarRoomMsg(scoutCh), waitForWarRoomMsg(challengerCh))
}

// ─── Key Handler ────────────────────────────────────────────────────────────

func (m WarRoomModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit

	case "esc":
		if m.focus != focusInput {
			m.focus = focusInput
			return m, nil
		}
		m.cancel()
		return m, tea.Quit

	case "tab":
		switch m.focus {
		case focusInput:
			m.focus = focusScout
		case focusScout:
			m.focus = focusChallenger
		case focusChallenger:
			m.focus = focusInput
		}
		return m, nil

	case "up":
		if m.focus == focusScout && m.scoutScroll > 0 {
			m.scoutScroll--
		} else if m.focus == focusChallenger && m.challengerScroll > 0 {
			m.challengerScroll--
		}
		return m, nil

	case "k":
		if m.focus != focusInput {
			if m.focus == focusScout && m.scoutScroll > 0 {
				m.scoutScroll--
			} else if m.focus == focusChallenger && m.challengerScroll > 0 {
				m.challengerScroll--
			}
			return m, nil
		}
		return m.insertInputChar('k')

	case "down":
		switch m.focus {
		case focusScout:
			vis := m.panelViewHeight()
			if max := len(m.scoutLines) - vis; max > 0 && m.scoutScroll < max {
				m.scoutScroll++
			}
		case focusChallenger:
			vis := m.panelViewHeight()
			if max := len(m.challengerLines) - vis; max > 0 && m.challengerScroll < max {
				m.challengerScroll++
			}
		}
		return m, nil

	case "j":
		if m.focus != focusInput {
			vis := m.panelViewHeight()
			switch m.focus {
			case focusScout:
				if max := len(m.scoutLines) - vis; max > 0 && m.scoutScroll < max {
					m.scoutScroll++
				}
			case focusChallenger:
				if max := len(m.challengerLines) - vis; max > 0 && m.challengerScroll < max {
					m.challengerScroll++
				}
			}
			return m, nil
		}
		return m.insertInputChar('j')

	case "pgup", "ctrl+b":
		vis := m.panelViewHeight()
		step := vis - 1
		if step < 1 {
			step = 1
		}
		switch m.focus {
		case focusScout:
			m.scoutScroll -= step
			if m.scoutScroll < 0 {
				m.scoutScroll = 0
			}
		case focusChallenger:
			m.challengerScroll -= step
			if m.challengerScroll < 0 {
				m.challengerScroll = 0
			}
		}
		return m, nil

	case "pgdown", "ctrl+f":
		vis := m.panelViewHeight()
		step := vis - 1
		if step < 1 {
			step = 1
		}
		switch m.focus {
		case focusScout:
			if max := len(m.scoutLines) - vis; max > 0 {
				m.scoutScroll += step
				if m.scoutScroll > max {
					m.scoutScroll = max
				}
			}
		case focusChallenger:
			if max := len(m.challengerLines) - vis; max > 0 {
				m.challengerScroll += step
				if m.challengerScroll > max {
					m.challengerScroll = max
				}
			}
		}
		return m, nil

	case "home":
		switch m.focus {
		case focusScout:
			m.scoutScroll = 0
		case focusChallenger:
			m.challengerScroll = 0
		}
		return m, nil

	case "g":
		if m.focus != focusInput {
			switch m.focus {
			case focusScout:
				m.scoutScroll = 0
			case focusChallenger:
				m.challengerScroll = 0
			}
			return m, nil
		}
		return m.insertInputChar('g')

	case "end":
		switch m.focus {
		case focusScout:
			m.pinScoutBottom()
		case focusChallenger:
			m.pinChallengerBottom()
		}
		return m, nil

	case "G":
		if m.focus != focusInput {
			switch m.focus {
			case focusScout:
				m.pinScoutBottom()
			case focusChallenger:
				m.pinChallengerBottom()
			}
			return m, nil
		}
		return m.insertInputChar('G')

	case "enter":
		if m.focus != focusInput {
			return m, nil
		}
		text := strings.TrimSpace(string(m.inputBuf))
		if text == "" {
			return m, nil
		}
		if text == "q" || text == "quit" || text == "exit" {
			m.cancel()
			return m, tea.Quit
		}
		return m.dispatch(text)

	case "backspace":
		if m.focus == focusInput && m.cursorPos > 0 {
			m.inputBuf = append(m.inputBuf[:m.cursorPos-1], m.inputBuf[m.cursorPos:]...)
			m.cursorPos--
		}
		return m, nil

	case "ctrl+a":
		if m.focus == focusInput {
			m.cursorPos = 0
		}
		return m, nil

	case "ctrl+e":
		if m.focus == focusInput {
			m.cursorPos = len(m.inputBuf)
		}
		return m, nil

	case "ctrl+u":
		if m.focus == focusInput {
			m.inputBuf = m.inputBuf[:0]
			m.cursorPos = 0
		}
		return m, nil

	case "left":
		if m.focus == focusInput && m.cursorPos > 0 {
			m.cursorPos--
		}
		return m, nil

	case "right":
		if m.focus == focusInput && m.cursorPos < len(m.inputBuf) {
			m.cursorPos++
		}
		return m, nil

	default:
		if m.focus == focusInput {
			if len(key) == 1 {
				return m.insertInputChar([]rune(key)[0])
			} else if key == "space" {
				return m.insertInputChar(' ')
			}
		}
		return m, nil
	}
}

// insertInputChar inserts r at the current cursor position in the input buffer.
func (m WarRoomModel) insertInputChar(r rune) (tea.Model, tea.Cmd) {
	tail := make([]rune, len(m.inputBuf)-m.cursorPos)
	copy(tail, m.inputBuf[m.cursorPos:])
	m.inputBuf = append(m.inputBuf[:m.cursorPos], r)
	m.inputBuf = append(m.inputBuf, tail...)
	m.cursorPos++
	return m, nil
}

// ─── Stream Agent (channel-based) ───────────────────────────────────────────

// waitForWarRoomMsg returns a Cmd that blocks until the next message arrives on
// ch, then delivers it to Bubbletea. This forms the streaming delivery chain:
// after each chunk Update calls waitForWarRoomMsg, which blocks for the next
// chunk, which triggers another Update, and so on — exactly like pilot.go.
func waitForWarRoomMsg(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			// Channel closed without a terminal message; treat as done.
			// This should not happen in normal operation since streamAgentToChannel
			// always sends done/error before closing.
			return nil
		}
		return msg
	}
}

// streamAgentToChannel runs an SSE chat stream and delivers all messages to ch.
// It always closes ch when it exits, and always sends a done/error msg before
// closing so waitForWarRoomMsg never blocks on a closed-and-empty channel.
func streamAgentToChannel(
	ctx context.Context,
	gatewayURL, gatewayToken, model, provider, sessionID,
	message, suffix, disposition string,
	isScout bool,
	ch chan<- tea.Msg,
) {
	defer close(ch)

	sendMsg := func(scout, challenger tea.Msg) {
		if isScout {
			ch <- scout
		} else {
			ch <- challenger
		}
	}

	reqBody := warRoomChatRequest{
		Message:            message,
		Model:              model,
		Provider:           provider,
		Stream:             true,
		SessionID:          sessionID,
		SystemPromptSuffix: suffix,
		Disposition:        disposition,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		sendMsg(scoutErrorMsg{err: err}, challengerErrorMsg{err: err})
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		gatewayURL+"/v1/chat", bytes.NewReader(body))
	if err != nil {
		sendMsg(scoutErrorMsg{err: err}, challengerErrorMsg{err: err})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if gatewayToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+gatewayToken)
	}

	resp, err := (&http.Client{}).Do(httpReq)
	if err != nil {
		sendMsg(scoutErrorMsg{err: err}, challengerErrorMsg{err: err})
		return
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort cleanup; stream is already fully consumed or being abandoned on error

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		e := fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		sendMsg(scoutErrorMsg{err: e}, challengerErrorMsg{err: e})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				sendMsg(scoutDoneMsg{}, challengerDoneMsg{})
				return
			}
			text := extractWarRoomText(event, data)
			if text != "" {
				sendMsg(scoutChunkMsg(text), challengerChunkMsg(text))
			}
		case line == "":
			event = ""
		}
	}
	if err := scanner.Err(); err != nil {
		sendMsg(scoutErrorMsg{err: err}, challengerErrorMsg{err: err})
		return
	}
	sendMsg(scoutDoneMsg{}, challengerDoneMsg{})
}

// extractWarRoomText pulls readable text from an SSE data field.
func extractWarRoomText(event, data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}

	switch event {
	case "thinking", "tool_invoked", "tool_completed":
		return ""
	case "error":
		return "[error: " + data + "]\n"
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err == nil {
		for _, key := range []string{"text", "content", "message", "delta"} {
			if v, ok := m[key].(string); ok {
				return v
			}
		}
		return ""
	}
	return data
}

// ─── View ───────────────────────────────────────────────────────────────────

func (m WarRoomModel) View() string {
	if m.width == 0 {
		return "  Initializing War Room...\n"
	}

	var sb strings.Builder

	// ── Header ──
	header := wrTitle.Render("  War Room")
	sb.WriteString(header + "\n")
	sb.WriteString(styleSubtle.Render(strings.Repeat("\u2500", m.width)) + "\n")

	// ── Panel dimensions ──
	panelWidth := m.panelWidth()
	panelHeight := m.panelViewHeight()
	gap := 1
	if m.width < 60 {
		gap = 0
	}

	// ── Scout panel ──
	scoutHeader := wrScoutLabel.Render("Scout")
	if m.scoutStreaming {
		scoutHeader += " " + wrStreamingDot.Render("\u25cf")
	} else if len(m.scoutLines) > 0 {
		scoutHeader += styleSubtle.Render(" (done)")
	}
	if m.focus == focusScout {
		scoutHeader += wrScrollHint.Render(m.scrollHint(m.scoutLines, m.scoutScroll))
	}

	scoutContent := m.renderPanelContent(m.scoutLines, m.scoutScroll, panelWidth-4, panelHeight)

	scoutBorderStyle := wrScoutBorder
	if m.focus == focusScout {
		scoutBorderStyle = wrScoutBorderActive
	}
	scoutPanel := scoutBorderStyle.
		Width(panelWidth - 2).
		Height(panelHeight + 1).
		Render(scoutHeader + "\n" + scoutContent)

	// ── Challenger panel ──
	challengerHeader := wrChallengerLabel.Render("Challenger")
	if m.challengerStreaming {
		challengerHeader += " " + wrStreamingDot.Render("\u25cf")
	} else if len(m.challengerLines) > 0 {
		challengerHeader += styleSubtle.Render(" (done)")
	}
	if m.focus == focusChallenger {
		challengerHeader += wrScrollHint.Render(m.scrollHint(m.challengerLines, m.challengerScroll))
	}

	challengerContent := m.renderPanelContent(m.challengerLines, m.challengerScroll, panelWidth-4, panelHeight)

	challengerBorderStyle := wrChallengerBorder
	if m.focus == focusChallenger {
		challengerBorderStyle = wrChallengerBorderActive
	}
	challengerPanel := challengerBorderStyle.
		Width(panelWidth - 2).
		Height(panelHeight + 1).
		Render(challengerHeader + "\n" + challengerContent)

	// Join panels side by side
	panels := lipgloss.JoinHorizontal(lipgloss.Top, scoutPanel, strings.Repeat(" ", gap), challengerPanel)
	sb.WriteString(panels + "\n")

	// ── Input bar ──
	inputWidth := m.width - 4
	if inputWidth < 10 {
		inputWidth = 10
	}
	prompt := wrInputPrompt.Render("> ")
	inputText := string(m.inputBuf)
	if m.focus == focusInput {
		before := string(m.inputBuf[:m.cursorPos])
		after := ""
		if m.cursorPos < len(m.inputBuf) {
			after = string(m.inputBuf[m.cursorPos:])
		}
		inputText = before + "\u2588" + after
	}
	inputLine := prompt + inputText
	if lipgloss.Width(inputLine) > inputWidth {
		inputLine = inputLine[:inputWidth]
	}
	inputBarStyle := wrInputBorder
	if m.focus == focusInput {
		inputBarStyle = wrInputBorderFocused
	}
	inputBar := inputBarStyle.Width(m.width - 4).Render(inputLine)

	// Streaming guard notice
	if m.scoutStreaming || m.challengerStreaming {
		inputBar = wrInputBorder.Width(m.width - 4).Render(
			styleSubtle.Render("  Agents responding... press tab to scroll panels"),
		)
	}
	sb.WriteString(inputBar + "\n")

	// ── Status bar ──
	var focusLabel string
	switch m.focus {
	case focusInput:
		focusLabel = "input"
	case focusScout:
		focusLabel = "scout"
	case focusChallenger:
		focusLabel = "challenger"
	}
	status := wrStatusBar.Render(fmt.Sprintf(
		"  focus: %s   [enter: send] [tab: cycle] [↑↓/pgup/pgdn: scroll] [g/G: top/bottom] [esc: quit]",
		focusLabel,
	))
	sb.WriteString(status + "\n")

	return sb.String()
}

// scrollHint returns a short "line X/Y" indicator for a panel.
func (m WarRoomModel) scrollHint(lines []string, scroll int) string {
	if len(lines) == 0 {
		return ""
	}
	vis := m.panelViewHeight()
	total := len(lines)
	current := scroll + vis
	if current > total {
		current = total
	}
	return fmt.Sprintf("  %d/%d", current, total)
}

func (m WarRoomModel) renderPanelContent(lines []string, scroll, width, height int) string {
	if len(lines) == 0 {
		return styleSubtle.Render("Awaiting input...")
	}

	start := scroll
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	if start >= len(lines) {
		start = 0
		end = 0
	}

	visible := lines[start:end]

	var sb strings.Builder
	for i, line := range visible {
		if i > 0 {
			sb.WriteString("\n")
		}
		if lipgloss.Width(line) > width {
			runes := []rune(line)
			if len(runes) > width {
				line = string(runes[:width])
			}
		}
		sb.WriteString(styleEventData.Render(line))
	}

	// Pad remaining lines
	for i := len(visible); i < height; i++ {
		sb.WriteString("\n")
	}

	return sb.String()
}

// ─── Layout Helpers ─────────────────────────────────────────────────────────

func (m WarRoomModel) panelWidth() int {
	w := (m.width - 1) / 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m WarRoomModel) panelContentWidth() int {
	return m.panelWidth() - 6 // border + padding
}

func (m WarRoomModel) panelViewHeight() int {
	// Total height minus: header(2) + input(3) + status(1)
	h := m.height - 6
	if h < 3 {
		h = 3
	}
	return h
}

// wrapText splits text into lines that fit within maxWidth characters.
func wrapText(text string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 40
	}

	rawLines := strings.Split(text, "\n")
	var result []string

	for _, raw := range rawLines {
		if raw == "" {
			result = append(result, "")
			continue
		}
		runes := []rune(raw)
		for len(runes) > maxWidth {
			breakAt := maxWidth
			for i := maxWidth; i > maxWidth/2; i-- {
				if runes[i] == ' ' {
					breakAt = i
					break
				}
			}
			result = append(result, string(runes[:breakAt]))
			runes = runes[breakAt:]
			if len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		}
		result = append(result, string(runes))
	}

	if len(result) > maxPanelLines {
		result = result[len(result)-maxPanelLines:]
	}

	return result
}
