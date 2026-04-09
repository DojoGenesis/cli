package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DojoGenesis/dojo-cli/internal/art"
	"github.com/DojoGenesis/dojo-cli/internal/client"
	"github.com/DojoGenesis/dojo-cli/internal/config"
)

// ─── Styles (home-specific) ──────────────────────────────────────────────────

var (
	styleHomeHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAmber)).
			MarginBottom(1)

	styleLabel = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray)).
			Width(20)

	styleValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorWhite))

	styleValueOK = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorGreen))

	styleValueWarn = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorRed))

	styleHomePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			Padding(0, 2)

	styleHomeSubtle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle))
)

// ─── Messages ────────────────────────────────────────────────────────────────

// homeDataMsg carries the results of the one-shot data fetch.
type homeDataMsg struct {
	gatewayStatus string
	agentCount    int
	seedCount     int
	fetchErr      error
}

// ─── Model ───────────────────────────────────────────────────────────────────

// HomeModel is a Bubbletea model for the static /home dashboard.
// It performs a one-shot health + agent + seed fetch on Init(), renders the
// results, and exits on any keypress. No live updates.
type HomeModel struct {
	cfg         *config.Config
	gw          *client.Client
	session     string
	pluginCount int

	// resolved after fetch
	workspace     string
	gatewayStatus string
	agentCount    int
	seedCount     int
	fetchErr      error
	ready         bool

	width  int
	height int
}

// NewHomeModel constructs a HomeModel.
// cfg is the loaded CLI config; gw is an initialised gateway client;
// session is the current REPL session ID; pluginCount is the number of loaded plugins.
func NewHomeModel(cfg *config.Config, gw *client.Client, session string, pluginCount int) HomeModel {
	// Derive workspace name from cwd basename.
	cwd, _ := os.Getwd()
	workspace := filepath.Base(cwd)

	return HomeModel{
		cfg:         cfg,
		gw:          gw,
		session:     session,
		pluginCount: pluginCount,
		workspace:   workspace,
	}
}

// Init fires the one-shot data-fetch command.
func (m HomeModel) Init() tea.Cmd {
	return m.fetchData()
}

// fetchData returns a Cmd that performs a quick health + agent + seed check
// against the gateway and returns a homeDataMsg.
func (m HomeModel) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		msg := homeDataMsg{}

		// Health check.
		h, err := m.gw.Health(ctx)
		if err != nil {
			msg.gatewayStatus = "unreachable"
			msg.fetchErr = err
			return msg
		}
		msg.gatewayStatus = h.Status

		// Agent count.
		agents, err := m.gw.Agents(ctx)
		if err == nil {
			msg.agentCount = len(agents)
		}

		// Seed count.
		seeds, err := m.gw.Seeds(ctx)
		if err == nil {
			msg.seedCount = len(seeds)
		}

		return msg
	}
}

// Update handles Bubbletea messages.
func (m HomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case homeDataMsg:
		m.gatewayStatus = msg.gatewayStatus
		m.agentCount = msg.agentCount
		m.seedCount = msg.seedCount
		m.fetchErr = msg.fetchErr
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		// Any keypress exits the dashboard.
		return m, tea.Quit
	}

	return m, nil
}

// View renders the static home dashboard.
func (m HomeModel) View() string {
	if !m.ready {
		return styleHomeSubtle.Render("  Loading workspace data…") + "\n"
	}

	var inner strings.Builder

	// ── Bonsai ──
	inner.WriteString(art.MediumBonsaiString())

	// ── Title ──
	inner.WriteString(styleHomeHeader.Render("  Dojo Workspace") + "\n")

	// ── Rows ──
	row := func(label, value string, style lipgloss.Style) string {
		return fmt.Sprintf("  %s  %s\n",
			styleLabel.Render(label),
			style.Render(value),
		)
	}

	inner.WriteString(row("Workspace", m.workspace, styleValue))

	// Gateway status row — green if "ok", red otherwise.
	gwStyle := styleValueOK
	if m.gatewayStatus != "ok" && m.gatewayStatus != "healthy" {
		gwStyle = styleValueWarn
	}
	inner.WriteString(row("Gateway", m.gatewayStatus, gwStyle))
	inner.WriteString(row("Gateway URL", m.cfg.Gateway.URL, styleValue))

	inner.WriteString(row("Agents", fmt.Sprintf("%d", m.agentCount), styleValue))
	inner.WriteString(row("Seeds", fmt.Sprintf("%d", m.seedCount), styleValue))
	inner.WriteString(row("Plugins", fmt.Sprintf("%d", m.pluginCount), styleValue))

	inner.WriteString(row("Session", m.session, styleValue))

	// Model / provider.
	model := m.cfg.Defaults.Model
	if model == "" {
		model = "(gateway default)"
	}
	provider := m.cfg.Defaults.Provider
	if provider == "" {
		provider = "(gateway default)"
	}
	inner.WriteString(row("Model", model, styleValue))
	inner.WriteString(row("Provider", provider, styleValue))

	if m.fetchErr != nil {
		inner.WriteString("\n  " + styleValueWarn.Render("  warning: "+m.fetchErr.Error()) + "\n")
	}

	// ── Footer hint ──
	inner.WriteString("\n  " + styleHomeSubtle.Render("Press any key to close") + "\n")

	// Wrap in a rounded panel.
	panelWidth := 60
	if m.width > 0 && m.width < panelWidth+4 {
		panelWidth = m.width - 4
	}

	panel := styleHomePanel.Width(panelWidth).Render(inner.String())
	return "\n" + panel + "\n"
}
