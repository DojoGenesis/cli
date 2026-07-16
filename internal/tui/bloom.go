package tui

// bloom.go — fullscreen animated bonsai garden TUI (/bloom command).
// Renders a living bonsai tree with falling petals, fireflies, wind,
// and an evolving stats panel tied to the Spirit progression system.

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DojoGenesis/cli/internal/art"
	"github.com/DojoGenesis/cli/internal/spirit"
)

// ─── Bloom tick message ────────────────────────────────────────────────────

type bloomTickMsg time.Time

// ─── Bloom styles ──────────────────────────────────────────────────────────

var (
	bloomBeltStyle = func(color string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
	}
	bloomXPFilled = lipgloss.NewStyle().Foreground(lipgloss.Color("#e8b04a"))
	bloomXPEmpty  = lipgloss.NewStyle().Foreground(lipgloss.Color("#334155"))
	bloomDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	bloomKoan     = lipgloss.NewStyle().Foreground(lipgloss.Color("#e8b04a")).Italic(true)
	bloomNight    = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	bloomMedTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e8b04a")) //nolint:unused // pending: medium-stage title heading, not yet wired into the bloom view
)

// ─── BloomModel ────────────────────────────────────────────────────────────

// BloomModel is the Bubbletea model for the animated bonsai garden scene.
type BloomModel struct {
	width, height int
	engine        *art.ParticleEngine
	stage         int // 0-7 from belt rank
	frame         int // global frame counter
	swayPhase     int // 0=left, 1=center, 2=right
	windActive    int // frames of wind remaining
	nightMode     bool
	meditating    bool
	previewStage  int // -1 = show real stage
	spirit        spirit.SpiritState
	belt          spirit.Belt
	koan          string
	koanReveal    int // chars revealed so far (typewriter)
	koanTimer     int // frames until next koan
	idleFrames    int // frames with no keypress
	birdActive    int // frames of bird visible remaining
	birdX, birdY  int // bird position
	startTime     time.Time
	rng           *rand.Rand
}

// NewBloomModel constructs a BloomModel ready for tea.NewProgram.
func NewBloomModel(sp spirit.SpiritState, belt spirit.Belt) BloomModel {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	koan := spirit.RandomKoan(belt.Rank, time.Now())
	return BloomModel{
		engine:       art.NewParticleEngine(),
		stage:        belt.Rank,
		previewStage: -1,
		spirit:       sp,
		belt:         belt,
		koan:         koan,
		koanTimer:    375, // ~30 seconds at 12.5 FPS
		startTime:    time.Now(),
		rng:          rng,
	}
}

// Init starts the tick loop.
func (m BloomModel) Init() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return bloomTickMsg(t)
	})
}

// ─── Update ────────────────────────────────────────────────────────────────

func (m BloomModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		m.idleFrames = 0
		m.birdActive = 0

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "w":
			m.windActive = 40
		case " ":
			stage := m.activeStage()
			sp := art.StageSpawnPoints(stage)
			m.engine.ShakeBurst(10, sp)
		case "f":
			stage := m.activeStage()
			baseX := art.StageWidth(stage) / 2
			baseY := art.StageHeight(stage)
			m.engine.SpawnFireflyBurst(5, baseX, baseY)
		case "n":
			m.nightMode = !m.nightMode
		case "m":
			m.meditating = !m.meditating
		case "1":
			m.togglePreview(0)
		case "2":
			m.togglePreview(1)
		case "3":
			m.togglePreview(2)
		case "4":
			m.togglePreview(3)
		case "5":
			m.togglePreview(4)
		case "6":
			m.togglePreview(5)
		case "7":
			m.togglePreview(6)
		case "8":
			m.togglePreview(7)
		}
		return m, nil

	case bloomTickMsg:
		m.updateTick()
		return m, tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
			return bloomTickMsg(t)
		})
	}

	return m, nil
}

// togglePreview sets or clears the preview stage.
func (m *BloomModel) togglePreview(stage int) {
	if m.previewStage == stage {
		m.previewStage = -1
	} else {
		m.previewStage = stage
	}
}

// activeStage returns the currently displayed stage.
func (m *BloomModel) activeStage() int {
	if m.previewStage >= 0 {
		return m.previewStage
	}
	return m.stage
}

// updateTick processes one frame of animation.
func (m *BloomModel) updateTick() {
	m.frame++

	// Meditation mode: skip every other tick for half-speed.
	if m.meditating && m.frame%2 != 0 {
		return
	}

	stage := m.activeStage()
	spawnPoints := art.StageSpawnPoints(stage)

	// Spawn petals: higher rank = more frequent. Base rate every 8 frames,
	// adjusted down by rank (rank 0 = every 15 frames, rank 7 = every 3).
	spawnRate := 15 - stage*2
	if spawnRate < 3 {
		spawnRate = 3
	}
	if m.frame%spawnRate == 0 {
		m.engine.SpawnPetal(spawnPoints)
	}

	// Spawn fireflies periodically.
	fireflyRate := 35 - stage*3
	if fireflyRate < 10 {
		fireflyRate = 10
	}
	if m.frame%fireflyRate == 0 {
		baseX := art.StageWidth(stage) / 2
		baseY := art.StageHeight(stage)
		m.engine.SpawnFirefly(baseX, baseY)
	}

	// Wind strength.
	var wind float64
	if m.windActive > 0 {
		m.windActive--
		wind = 0.3
	}

	// Ground line Y.
	groundY := m.groundY()
	m.engine.Update(groundY, wind)

	// Canopy sway: every 25 frames cycle phase.
	if m.frame%25 == 0 {
		m.swayPhase = (m.swayPhase + 1) % 3
	}

	// Koan rotation.
	m.koanTimer--
	if m.koanTimer <= 0 {
		m.koan = spirit.RandomKoan(m.belt.Rank, time.Now())
		m.koanReveal = 0
		m.koanTimer = 375
	}

	// Koan typewriter reveal.
	if m.koanReveal < len([]rune(m.koan)) {
		m.koanReveal++
	}

	// Idle detection.
	m.idleFrames++
	if m.idleFrames >= 750 && m.birdActive == 0 {
		m.birdActive = 100 // ~8 seconds
		m.birdX = 2
		if m.height > 5 {
			m.birdY = m.rng.Intn(m.height / 3)
		}
	}

	// Bird animation.
	if m.birdActive > 0 {
		m.birdActive--
		m.birdX++
	}
}

// groundY returns the Y coordinate for the ground line.
func (m *BloomModel) groundY() int {
	stage := m.activeStage()
	treeH := art.StageHeight(stage)
	// Ground sits 2 lines below the tree bottom.
	gy := (m.height+treeH)/2 + 2
	if gy >= m.height-3 {
		gy = m.height - 4
	}
	if gy < treeH+2 {
		gy = treeH + 2
	}
	return gy
}

// ─── View ──────────────────────────────────────────────────────────────────

func (m BloomModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "  Loading bloom garden...\n"
	}

	// Graceful degradation for tiny terminals.
	if m.width < 40 || m.height < 15 {
		return bloomDim.Render("  Terminal too small for bloom.\n  Need at least 40x15.\n  Press q to quit.")
	}

	// Meditation overlay.
	if m.meditating {
		return m.viewMeditation()
	}

	stage := m.activeStage()
	statsW := 26
	sceneW := m.width - statsW
	if sceneW < 20 {
		sceneW = 20
		statsW = m.width - sceneW
	}

	// Build particle overlay map: (x,y) -> (char, colorHex).
	type particle struct {
		ch    rune
		color string
	}
	pmap := make(map[[2]int]particle)

	// Falling petals.
	for _, p := range m.engine.Petals {
		px, py := int(p.X), int(p.Y)
		if px >= 0 && px < sceneW && py >= 0 && py < m.height {
			pmap[[2]int{px, py}] = particle{ch: p.Char, color: art.PetalColorHex(p.ColorID)}
		}
	}

	// Ground petals.
	gy := m.groundY()
	for _, gp := range m.engine.Ground {
		if gp.X >= 0 && gp.X < sceneW && gy >= 0 && gy < m.height {
			pmap[[2]int{gp.X, gy}] = particle{ch: gp.Char, color: art.PetalColorHex(gp.ColorID)}
		}
	}

	// Fireflies.
	for _, ff := range m.engine.Fireflies {
		fx, fy := int(ff.X), int(ff.Y)
		// Blink: visible when Phase % 16 < 10 (or always in nightMode).
		visible := ff.Phase%16 < 10 || m.nightMode
		if visible && fx >= 0 && fx < sceneW && fy >= 0 && fy < m.height {
			pmap[[2]int{fx, fy}] = particle{ch: '\u2726', color: "#ffd166"} // ✦ gold
		}
	}

	// Bird.
	if m.birdActive > 0 && m.birdX >= 0 && m.birdX < sceneW && m.birdY >= 0 && m.birdY < m.height {
		pmap[[2]int{m.birdX, m.birdY}] = particle{ch: '>', color: "#94a3b8"}
	}

	// Get tree lines.
	treeLines := art.StageLines(stage, m.swayPhase)
	treeW := art.StageWidth(stage)
	treeH := len(treeLines)

	// Center tree horizontally in scene area.
	treeOffX := (sceneW - treeW) / 2
	if treeOffX < 0 {
		treeOffX = 0
	}
	// Center tree vertically, slightly above center.
	treeOffY := (m.height - treeH) / 2
	if treeOffY < 1 {
		treeOffY = 1
	}

	// Build tree colored text (pre-split into lines for overlay).
	treeColoredLines := strings.Split(art.StageColored(stage, m.swayPhase), "\n")
	// Trim trailing empty line if present.
	if len(treeColoredLines) > 0 && treeColoredLines[len(treeColoredLines)-1] == "" {
		treeColoredLines = treeColoredLines[:len(treeColoredLines)-1]
	}

	// Build stats panel lines.
	statsLines := m.buildStatsPanel(statsW)

	// Build the full screen.
	var sb strings.Builder
	for row := 0; row < m.height; row++ {
		// Build scene portion (left side).
		var sceneLine strings.Builder

		// Determine what this row contains.
		treeRow := row - treeOffY
		isTreeRow := treeRow >= 0 && treeRow < treeH
		isGroundRow := row == gy

		if isTreeRow && treeRow < len(treeColoredLines) {
			// Render: leading space + tree colored line + trailing space,
			// but check for particle overlays at each column.
			rawLine := ""
			if treeRow < len(treeLines) {
				rawLine = treeLines[treeRow]
			}

			// Check if any particles overlap this tree row.
			hasOverlay := false
			for col := 0; col < sceneW; col++ {
				if _, ok := pmap[[2]int{col, row}]; ok {
					hasOverlay = true
					break
				}
			}

			if !hasOverlay {
				// Fast path: just use the pre-colored tree line with padding.
				pad := strings.Repeat(" ", treeOffX)
				lineContent := pad + treeColoredLines[treeRow]
				sceneLine.WriteString(lineContent)
			} else {
				// Slow path: character-by-character with overlay.
				runes := []rune(rawLine)
				for col := 0; col < sceneW; col++ {
					if p, ok := pmap[[2]int{col, row}]; ok {
						sceneLine.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(p.color)).Render(string(p.ch)))
					} else {
						treeCol := col - treeOffX
						if treeCol >= 0 && treeCol < len(runes) && runes[treeCol] != ' ' {
							// Use colored tree character — for simplicity, render
							// the whole colored line in the fast path above. When
							// overlays exist, fall back to uncolored tree chars.
							sceneLine.WriteRune(runes[treeCol])
						} else if m.nightMode && m.rng.Intn(80) == 0 {
							sceneLine.WriteString(bloomNight.Render("\u00b7"))
						} else {
							sceneLine.WriteRune(' ')
						}
					}
				}
			}
		} else if isGroundRow {
			// Ground line.
			for col := 0; col < sceneW; col++ {
				if p, ok := pmap[[2]int{col, row}]; ok {
					sceneLine.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(p.color)).Render(string(p.ch)))
				} else if col%7 == 3 {
					sceneLine.WriteString(bloomDim.Render(","))
				} else {
					sceneLine.WriteString(bloomDim.Render("\u2500"))
				}
			}
		} else {
			// Empty row (background).
			for col := 0; col < sceneW; col++ {
				if p, ok := pmap[[2]int{col, row}]; ok {
					sceneLine.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(p.color)).Render(string(p.ch)))
				} else if m.nightMode && m.rng.Intn(60) == 0 {
					sceneLine.WriteString(bloomNight.Render("\u00b7"))
				} else {
					sceneLine.WriteRune(' ')
				}
			}
		}

		// Pad scene line to sceneW visual width.
		sceneStr := sceneLine.String()
		visW := lipgloss.Width(sceneStr)
		if visW < sceneW {
			sceneStr += strings.Repeat(" ", sceneW-visW)
		}

		// Stats panel for this row.
		statsStr := ""
		if row < len(statsLines) {
			statsStr = statsLines[row]
		}
		// Pad stats to statsW.
		statsVisW := lipgloss.Width(statsStr)
		if statsVisW < statsW {
			statsStr += strings.Repeat(" ", statsW-statsVisW)
		}

		sb.WriteString(sceneStr)
		sb.WriteString(statsStr)

		if row < m.height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// viewMeditation renders the meditation overlay.
func (m BloomModel) viewMeditation() string {
	var sb strings.Builder

	// Reveal koan text.
	koanRunes := []rune(m.koan)
	revealed := m.koanReveal
	if revealed > len(koanRunes) {
		revealed = len(koanRunes)
	}
	koanText := string(koanRunes[:revealed])

	// Word-wrap koan at ~50 chars.
	maxKoanW := 50
	if m.width-10 < maxKoanW {
		maxKoanW = m.width - 10
	}
	if maxKoanW < 20 {
		maxKoanW = 20
	}
	koanLines := wrapSimple(koanText, maxKoanW)

	// Total content height: belt line + blank + koan lines + blank + hint.
	contentH := 1 + 1 + len(koanLines) + 1 + 1
	startY := (m.height - contentH) / 2
	if startY < 0 {
		startY = 0
	}

	beltStr := bloomBeltStyle(m.belt.Color).Render(fmt.Sprintf("%s %s", m.belt.Name, m.belt.Title))

	for row := 0; row < m.height; row++ {
		rel := row - startY

		var line string
		switch {
		case rel == 0:
			line = centerStr(beltStr, m.width, lipgloss.Width(beltStr))
		case rel == 1:
			line = ""
		case rel >= 2 && rel < 2+len(koanLines):
			kl := bloomKoan.Render(koanLines[rel-2])
			line = centerStr(kl, m.width, lipgloss.Width(kl))
		case rel == 2+len(koanLines):
			line = ""
		case rel == 3+len(koanLines):
			hint := bloomDim.Render("m: exit meditation  q: quit")
			line = centerStr(hint, m.width, lipgloss.Width(hint))
		default:
			if m.nightMode && m.rng.Intn(100) == 0 {
				pad := m.rng.Intn(m.width)
				line = strings.Repeat(" ", pad) + bloomNight.Render("\u00b7")
			}
		}

		sb.WriteString(line)
		if row < m.height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// buildStatsPanel returns per-row strings for the right stats panel.
func (m BloomModel) buildStatsPanel(w int) []string {
	lines := make([]string, 0, m.height)
	pad := "  "

	// Belt name colored.
	beltStr := pad + bloomBeltStyle(m.belt.Color).Render(fmt.Sprintf("%s %s", m.belt.Name, m.belt.Title))
	lines = append(lines, beltStr)

	// Separator.
	sepW := w - 4
	if sepW < 10 {
		sepW = 10
	}
	lines = append(lines, pad+bloomDim.Render(strings.Repeat("\u2500", sepW)))

	// XP line.
	next, xpNeeded := spirit.NextBelt(m.spirit.XP)
	var xpLine string
	if next == nil {
		xpLine = fmt.Sprintf("XP  %d / MAX", m.spirit.XP)
	} else {
		xpLine = fmt.Sprintf("XP  %d / %d", m.spirit.XP, next.Threshold)
	}
	lines = append(lines, pad+xpLine)

	// Progress bar.
	pct := spirit.ProgressPercent(m.spirit.XP)
	barW := 12
	filled := int(pct * float64(barW))
	if filled > barW {
		filled = barW
	}
	bar := bloomXPFilled.Render(strings.Repeat("\u2588", filled)) +
		bloomXPEmpty.Render(strings.Repeat("\u2591", barW-filled))
	pctStr := fmt.Sprintf(" %d%%", int(pct*100))
	lines = append(lines, pad+bar+pctStr)

	// Blank.
	lines = append(lines, "")

	// Streak and sessions.
	lines = append(lines, pad+fmt.Sprintf("Streak: %d days", m.spirit.StreakDays))
	lines = append(lines, pad+fmt.Sprintf("Sessions: %d", m.spirit.TotalSessions))

	// Blank.
	lines = append(lines, "")

	// Next belt.
	if next != nil {
		lines = append(lines, pad+fmt.Sprintf("Next: %s", next.Name))
		lines = append(lines, pad+fmt.Sprintf("%d XP to go", xpNeeded))
	} else {
		lines = append(lines, pad+"Max rank reached")
		lines = append(lines, "")
	}

	// Separator.
	lines = append(lines, pad+bloomDim.Render(strings.Repeat("\u2500", sepW)))

	// Koan (typewriter reveal).
	koanRunes := []rune(m.koan)
	revealed := m.koanReveal
	if revealed > len(koanRunes) {
		revealed = len(koanRunes)
	}
	koanText := string(koanRunes[:revealed])

	// Word-wrap koan for stats panel.
	koanW := w - 6
	if koanW < 10 {
		koanW = 10
	}
	koanWrapped := wrapSimple(koanText, koanW)
	for _, kl := range koanWrapped {
		lines = append(lines, pad+bloomKoan.Render(kl))
	}

	// Blank.
	lines = append(lines, "")

	// Preview indicator.
	if m.previewStage >= 0 {
		pBelt := spirit.Belts[m.previewStage]
		lines = append(lines, pad+bloomDim.Render(fmt.Sprintf("Preview: %s", pBelt.Name)))
		lines = append(lines, "")
	}

	// Key hints.
	lines = append(lines, pad+bloomDim.Render("w:wind Space:shake"))
	lines = append(lines, pad+bloomDim.Render("f:fireflies n:night"))
	lines = append(lines, pad+bloomDim.Render("m:meditate  q:quit"))
	lines = append(lines, pad+bloomDim.Render("1-8:preview stages"))

	// Pad to full height.
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	return lines
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// wrapSimple wraps text at word boundaries to fit maxW characters per line.
func wrapSimple(text string, maxW int) []string {
	if maxW < 1 {
		maxW = 40
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > maxW {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return lines
}

// centerStr returns s centered within width, accounting for visWidth
// (the visual width which may differ from byte length due to ANSI codes).
func centerStr(s string, width, visWidth int) string {
	if visWidth >= width {
		return s
	}
	pad := (width - visWidth) / 2
	return strings.Repeat(" ", pad) + s
}
