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
	scoutSuffix     = "You are a strategic scout. Explore possibilities, find routes through the problem, and synthesize options. Be thorough and measured."
	challengerSuffix = "You are a professional challenger. Find the strongest objection to whatever was just proposed. Do not hedge. Lead with the objection. If you cannot find a genuine flaw, say so explicitly."
	maxPanelLines   = 500
)

// ─── War Room Styles ────────────────────────────────────────────────────────

var (
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
)

// ─── War Room Messages ──────────────────────────────────────────────────────

type scoutChunkMsg string
type challengerChunkMsg string
type scoutDoneMsg struct{}
type challengerDoneMsg struct{}
type scoutErrorMsg struct{ err error }
type challengerErrorMsg struct{ err error }

// ─── War Room chat request ──────────────────────────────────────────────────

// warRoomChatRequest is the request body for the war room's gateway calls.
// It extends the standard chat request with system_prompt_suffix and disposition.
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
	focusInput focusPanel = iota
	focusScout
	focusChallenger
)

// WarRoomModel is the Bubbletea model for the split-panel debate TUI.
type WarRoomModel struct {
	// Input
	inputBuf  []rune
	cursorPos int

	// Two agent panels
	scoutBuf      strings.Builder
	challengerBuf strings.Builder
	scoutLines    []string
	challengerLines []string
	scoutScroll      int
	challengerScroll int
	scoutStreaming   bool
	challengerStreaming bool

	// Focus
	focus focusPanel

	// Layout
	width  int
	height int

	// Gateway connection
	gatewayURL string
	gatewayToken string
	model      string
	provider   string
	sessionID  string

	// Context
	ctx    context.Context
	cancel context.CancelFunc

	// Error
	err error
}

// NewWarRoomModel constructs a WarRoomModel ready for tea.NewProgram.
func NewWarRoomModel(gatewayURL, gatewayToken, model, provider, sessionID string) WarRoomModel {
	ctx, cancel := context.WithCancel(context.Background())
	return WarRoomModel{
		inputBuf:   make([]rune, 0, 256),
		focus:      focusInput,
		gatewayURL: gatewayURL,
		gatewayToken: gatewayToken,
		model:      model,
		provider:   provider,
		sessionID:  sessionID,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Init returns nil — no startup command needed until the user sends a message.
func (m WarRoomModel) Init() tea.Cmd {
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

	case scoutChunkMsg:
		m.scoutBuf.WriteString(string(msg))
		m.scoutLines = wrapText(m.scoutBuf.String(), m.panelContentWidth())
		// Auto-scroll to bottom
		vis := m.panelViewHeight()
		if max := len(m.scoutLines) - vis; max > 0 && m.scoutScroll >= max-1 {
			m.scoutScroll = max
		}
		return m, nil

	case challengerChunkMsg:
		m.challengerBuf.WriteString(string(msg))
		m.challengerLines = wrapText(m.challengerBuf.String(), m.panelContentWidth())
		vis := m.panelViewHeight()
		if max := len(m.challengerLines) - vis; max > 0 && m.challengerScroll >= max-1 {
			m.challengerScroll = max
		}
		return m, nil

	case scoutDoneMsg:
		m.scoutStreaming = false
		return m, nil

	case challengerDoneMsg:
		m.challengerStreaming = false
		return m, nil

	case scoutErrorMsg:
		m.scoutStreaming = false
		m.scoutBuf.WriteString(fmt.Sprintf("\n[error: %v]", msg.err))
		m.scoutLines = wrapText(m.scoutBuf.String(), m.panelContentWidth())
		return m, nil

	case challengerErrorMsg:
		m.challengerStreaming = false
		m.challengerBuf.WriteString(fmt.Sprintf("\n[error: %v]", msg.err))
		m.challengerLines = wrapText(m.challengerBuf.String(), m.panelContentWidth())
		return m, nil
	}

	return m, nil
}

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

	case "up", "k":
		if m.focus == focusScout && m.scoutScroll > 0 {
			m.scoutScroll--
		} else if m.focus == focusChallenger && m.challengerScroll > 0 {
			m.challengerScroll--
		}
		return m, nil

	case "down", "j":
		if m.focus == focusScout {
			vis := m.panelViewHeight()
			if max := len(m.scoutLines) - vis; max > 0 && m.scoutScroll < max {
				m.scoutScroll++
			}
		} else if m.focus == focusChallenger {
			vis := m.panelViewHeight()
			if max := len(m.challengerLines) - vis; max > 0 && m.challengerScroll < max {
				m.challengerScroll++
			}
		}
		return m, nil

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
		// Clear input
		m.inputBuf = m.inputBuf[:0]
		m.cursorPos = 0

		// Clear previous responses
		m.scoutBuf.Reset()
		m.challengerBuf.Reset()
		m.scoutLines = nil
		m.challengerLines = nil
		m.scoutScroll = 0
		m.challengerScroll = 0
		m.scoutStreaming = true
		m.challengerStreaming = true

		// Launch both streams
		scoutCmd := m.streamAgent(text, scoutSuffix, "measured", true)
		challengerCmd := m.streamAgent(text, challengerSuffix, "adversarial", false)

		return m, tea.Batch(scoutCmd, challengerCmd)

	case "backspace":
		if m.focus == focusInput && m.cursorPos > 0 {
			m.inputBuf = append(m.inputBuf[:m.cursorPos-1], m.inputBuf[m.cursorPos:]...)
			m.cursorPos--
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
		if m.focus == focusInput && len(key) == 1 {
			// Insert character at cursor
			r := []rune(key)[0]
			tail := make([]rune, len(m.inputBuf)-m.cursorPos)
			copy(tail, m.inputBuf[m.cursorPos:])
			m.inputBuf = append(m.inputBuf[:m.cursorPos], r)
			m.inputBuf = append(m.inputBuf, tail...)
			m.cursorPos++
		} else if m.focus == focusInput && key == "space" {
			tail := make([]rune, len(m.inputBuf)-m.cursorPos)
			copy(tail, m.inputBuf[m.cursorPos:])
			m.inputBuf = append(m.inputBuf[:m.cursorPos], ' ')
			m.inputBuf = append(m.inputBuf, tail...)
			m.cursorPos++
		}
		return m, nil
	}
}

// ─── Stream Agent ───────────────────────────────────────────────────────────

func (m WarRoomModel) streamAgent(message, suffix, disposition string, isScout bool) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan tea.Msg, 64)

		go func() {
			defer close(ch)

			reqBody := warRoomChatRequest{
				Message:            message,
				Model:              m.model,
				Provider:           m.provider,
				Stream:             true,
				SessionID:          m.sessionID + "-" + disposition,
				SystemPromptSuffix: suffix,
				Disposition:        disposition,
			}

			body, err := json.Marshal(reqBody)
			if err != nil {
				if isScout {
					ch <- scoutErrorMsg{err: err}
				} else {
					ch <- challengerErrorMsg{err: err}
				}
				return
			}

			ctx, cancel := context.WithTimeout(m.ctx, 120*time.Second)
			defer cancel()

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.gatewayURL+"/v1/chat", bytes.NewReader(body))
			if err != nil {
				if isScout {
					ch <- scoutErrorMsg{err: err}
				} else {
					ch <- challengerErrorMsg{err: err}
				}
				return
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			if m.gatewayToken != "" {
				httpReq.Header.Set("Authorization", "Bearer "+m.gatewayToken)
			}

			httpClient := &http.Client{}
			resp, err := httpClient.Do(httpReq)
			if err != nil {
				if isScout {
					ch <- scoutErrorMsg{err: err}
				} else {
					ch <- challengerErrorMsg{err: err}
				}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(resp.Body)
				errMsg := fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
				if isScout {
					ch <- scoutErrorMsg{err: errMsg}
				} else {
					ch <- challengerErrorMsg{err: errMsg}
				}
				return
			}

			// Parse SSE
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
						if isScout {
							ch <- scoutDoneMsg{}
						} else {
							ch <- challengerDoneMsg{}
						}
						return
					}
					text := extractWarRoomText(event, data)
					if text != "" {
						if isScout {
							ch <- scoutChunkMsg(text)
						} else {
							ch <- challengerChunkMsg(text)
						}
					}
				case line == "":
					event = ""
				}
			}
			if err := scanner.Err(); err != nil {
				if isScout {
					ch <- scoutErrorMsg{err: err}
				} else {
					ch <- challengerErrorMsg{err: err}
				}
				return
			}
			if isScout {
				ch <- scoutDoneMsg{}
			} else {
				ch <- challengerDoneMsg{}
			}
		}()

		// Return first message, then set up forwarding
		first, ok := <-ch
		if !ok {
			if isScout {
				return scoutDoneMsg{}
			}
			return challengerDoneMsg{}
		}

		// Launch forwarder for remaining messages
		go func() {
			for msg := range ch {
				_ = msg // messages are consumed through the program's send
			}
		}()

		return first
	}
}

// extractWarRoomText pulls readable text from an SSE data field.
func extractWarRoomText(event, data string) string {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return ""
	}

	// Skip non-text events
	switch event {
	case "thinking":
		return ""
	case "tool_invoked", "tool_completed":
		return ""
	case "error":
		return "[error: " + data + "]\n"
	}

	// Try JSON extraction
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
	scoutHeader := wrScoutLabel.Render("Scout (measured)")
	if m.scoutStreaming {
		scoutHeader += " " + wrStreamingDot.Render("\u25cf")
	}

	scoutContent := m.renderPanelContent(m.scoutLines, m.scoutScroll, panelWidth-4, panelHeight)
	scoutPanel := wrScoutBorder.
		Width(panelWidth - 2).
		Height(panelHeight + 1).
		Render(scoutHeader + "\n" + scoutContent)

	// ── Challenger panel ──
	challengerHeader := wrChallengerLabel.Render("Challenger (adversarial)")
	if m.challengerStreaming {
		challengerHeader += " " + wrStreamingDot.Render("\u25cf")
	}

	challengerContent := m.renderPanelContent(m.challengerLines, m.challengerScroll, panelWidth-4, panelHeight)
	challengerPanel := wrChallengerBorder.
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
	// Show cursor
	if m.focus == focusInput {
		before := string(m.inputBuf[:m.cursorPos])
		after := ""
		if m.cursorPos < len(m.inputBuf) {
			after = string(m.inputBuf[m.cursorPos:])
		}
		inputText = before + "\u2588" + after
	}
	inputLine := prompt + inputText
	// Truncate if too wide
	if lipgloss.Width(inputLine) > inputWidth {
		inputLine = inputLine[:inputWidth]
	}
	inputBar := wrInputBorder.Width(m.width - 4).Render(inputLine)
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
		"  focus: %s   [enter: send] [tab: focus] [up/down: scroll] [esc/ctrl+c: quit]",
		focusLabel,
	))
	sb.WriteString(status + "\n")

	return sb.String()
}

func (m WarRoomModel) renderPanelContent(lines []string, scroll, width, height int) string {
	if len(lines) == 0 {
		placeholder := styleSubtle.Render("Awaiting input...")
		return placeholder
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
		// Truncate line to panel width
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
	// Split width in half, accounting for gap
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
			// Try to break at a space
			breakAt := maxWidth
			for i := maxWidth; i > maxWidth/2; i-- {
				if runes[i] == ' ' {
					breakAt = i
					break
				}
			}
			result = append(result, string(runes[:breakAt]))
			runes = runes[breakAt:]
			// Skip leading space after break
			if len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		}
		result = append(result, string(runes))
	}

	// Cap total lines
	if len(result) > maxPanelLines {
		result = result[len(result)-maxPanelLines:]
	}

	return result
}
