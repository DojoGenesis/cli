// Package art provides ASCII art assets for the dojo CLI.
package art

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Sunset palette ─────────────────────────────────────────────────────────

// Canopy colors: sunset gradient from golden to terracotta.
var (
	leafGold     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd166"))
	leafAmber    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f4a261"))
	leafTerra    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e76f51"))
	trunkBrown   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B6914"))
	trunkDark    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B4E12"))
	potColor     = lipgloss.NewStyle().Foreground(lipgloss.Color("#5C4033"))
	soilColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("#3E2723"))
	mossColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B8E23"))
)

// ─── Small Bonsai (welcome screen, 6 lines) ────────────────────────────────

// SmallBonsai returns a compact 6-line bonsai with a sunset-gradient canopy.
// Suitable for the welcome banner — minimal vertical footprint.
func SmallBonsai() string {
	var b strings.Builder

	//  line 1: crown tip
	b.WriteString("       ")
	b.WriteString(leafGold.Render(".:*~*:."))
	b.WriteByte('\n')

	// line 2: upper canopy
	b.WriteString("     ")
	b.WriteString(leafGold.Render(".:"))
	b.WriteString(leafAmber.Render("*'~'*'~"))
	b.WriteString(leafGold.Render(":."))
	b.WriteByte('\n')

	// line 3: lower canopy
	b.WriteString("    ")
	b.WriteString(leafAmber.Render("'*:."))
	b.WriteString(leafTerra.Render(".,~.,"))
	b.WriteString(leafAmber.Render(".:*'"))
	b.WriteByte('\n')

	// line 4: trunk top
	b.WriteString("        ")
	b.WriteString(trunkBrown.Render("|||"))
	b.WriteByte('\n')

	// line 5: trunk base
	b.WriteString("        ")
	b.WriteString(trunkDark.Render("|||"))
	b.WriteByte('\n')

	// line 6: pot
	b.WriteString("      ")
	b.WriteString(potColor.Render("_[___]_"))
	b.WriteByte('\n')

	return b.String()
}

// ─── Medium Bonsai (home screen, 10 lines) ──────────────────────────────────

// MediumBonsai returns a 10-line bonsai for the home dashboard panel.
// Uses lipgloss styles for sunset-gradient leaves and earthy trunk tones.
func MediumBonsai() string {
	var b strings.Builder

	// line 1: crown tip
	b.WriteString("          ")
	b.WriteString(leafGold.Render(".:*~."))
	b.WriteByte('\n')

	// line 2: upper canopy
	b.WriteString("       ")
	b.WriteString(leafGold.Render(".:*'"))
	b.WriteString(leafAmber.Render("~'*'~"))
	b.WriteString(leafGold.Render("*:."))
	b.WriteByte('\n')

	// line 3: mid canopy
	b.WriteString("     ")
	b.WriteString(leafAmber.Render(".:*'"))
	b.WriteString(leafTerra.Render(" ~.,.'~ "))
	b.WriteString(leafAmber.Render("'*:."))
	b.WriteByte('\n')

	// line 4: lower canopy
	b.WriteString("    ")
	b.WriteString(leafTerra.Render("'*:."))
	b.WriteString(leafAmber.Render("  .~*~.  "))
	b.WriteString(leafTerra.Render(".:*'"))
	b.WriteByte('\n')

	// line 5: canopy base
	b.WriteString("      ")
	b.WriteString(leafTerra.Render("'~.,"))
	b.WriteString(leafAmber.Render(".,~.,"))
	b.WriteString(leafTerra.Render(",~'"))
	b.WriteByte('\n')

	// line 6: upper trunk with branches
	b.WriteString("          ")
	b.WriteString(trunkBrown.Render("/|\\"))
	b.WriteByte('\n')

	// line 7: trunk
	b.WriteString("          ")
	b.WriteString(trunkBrown.Render("|||"))
	b.WriteByte('\n')

	// line 8: trunk base
	b.WriteString("         ")
	b.WriteString(trunkDark.Render("/|||\\"))
	b.WriteByte('\n')

	// line 9: pot rim
	b.WriteString("       ")
	b.WriteString(potColor.Render("_|=====|_"))
	b.WriteByte('\n')

	// line 10: pot base
	b.WriteString("        ")
	b.WriteString(potColor.Render("\\_____/"))
	b.WriteByte('\n')

	return b.String()
}

// ─── Large Bonsai (practice screen, 14 lines) ──────────────────────────────

// LargeBonsai returns an elaborate 14-line bonsai for the practice screen.
// The most detailed variant — fitting for the contemplative daily-reflection view.
func LargeBonsai() string {
	var b strings.Builder

	// line 1: crown tip
	b.WriteString("              ")
	b.WriteString(leafGold.Render(".:*~."))
	b.WriteByte('\n')

	// line 2: upper crown
	b.WriteString("           ")
	b.WriteString(leafGold.Render(".:*'~"))
	b.WriteString(leafAmber.Render("*'*"))
	b.WriteString(leafGold.Render("~'*:."))
	b.WriteByte('\n')

	// line 3: crown spread
	b.WriteString("        ")
	b.WriteString(leafGold.Render(".:*'"))
	b.WriteString(leafAmber.Render(" ~.,.'~.,"))
	b.WriteString(leafGold.Render(" '*:."))
	b.WriteByte('\n')

	// line 4: upper-mid canopy
	b.WriteString("      ")
	b.WriteString(leafAmber.Render(".:*'"))
	b.WriteString(leafTerra.Render("  '~.*.'~.*  "))
	b.WriteString(leafAmber.Render("'*:."))
	b.WriteByte('\n')

	// line 5: mid canopy
	b.WriteString("     ")
	b.WriteString(leafAmber.Render("'*:."))
	b.WriteString(leafTerra.Render("  .,~'*'~,.   "))
	b.WriteString(leafAmber.Render(".:*'"))
	b.WriteByte('\n')

	// line 6: lower canopy left branch
	b.WriteString("    ")
	b.WriteString(leafTerra.Render(".:*'"))
	b.WriteString(leafAmber.Render(".,"))
	b.WriteString(leafTerra.Render("  ~.,.'~  "))
	b.WriteString(leafAmber.Render(",."))
	b.WriteString(leafTerra.Render("'*:."))
	b.WriteByte('\n')

	// line 7: canopy floor
	b.WriteString("      ")
	b.WriteString(leafTerra.Render("'~.,"))
	b.WriteString(leafAmber.Render(".,~*'*~,."))
	b.WriteString(leafTerra.Render(",~'"))
	b.WriteByte('\n')

	// line 8: branch spread
	b.WriteString("          ")
	b.WriteString(trunkBrown.Render("__/|\\__"))
	b.WriteByte('\n')

	// line 9: upper trunk
	b.WriteString("            ")
	b.WriteString(trunkBrown.Render("/|\\"))
	b.WriteByte('\n')

	// line 10: mid trunk
	b.WriteString("            ")
	b.WriteString(trunkBrown.Render("|||"))
	b.WriteByte('\n')

	// line 11: lower trunk
	b.WriteString("           ")
	b.WriteString(trunkDark.Render("/|||\\"))
	b.WriteByte('\n')

	// line 12: pot rim
	b.WriteString("        ")
	b.WriteString(potColor.Render("__|=======|__"))
	b.WriteByte('\n')

	// line 13: pot body with moss
	b.WriteString("        ")
	b.WriteString(potColor.Render("|"))
	b.WriteString(mossColor.Render("  "))
	b.WriteString(soilColor.Render("~.,.,~"))
	b.WriteString(mossColor.Render("  "))
	b.WriteString(potColor.Render("|"))
	b.WriteByte('\n')

	// line 14: pot base
	b.WriteString("         ")
	b.WriteString(potColor.Render("\\_________/"))
	b.WriteByte('\n')

	return b.String()
}

// ─── Indented variants ──────────────────────────────────────────────────────

// Indent prepends each line of the given string with the specified prefix.
// Useful for aligning bonsai art within panels or beside other text.
func Indent(s string, prefix string) string {
	lines := strings.Split(s, "\n")
	var out strings.Builder
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		if line != "" {
			out.WriteString(prefix)
			out.WriteString(line)
		}
	}
	return out.String()
}

// SmallBonsaiIndented returns a compact bonsai indented with the given prefix.
func SmallBonsaiIndented(prefix string) string {
	return Indent(SmallBonsai(), prefix)
}

// MediumBonsaiIndented returns a medium bonsai indented with the given prefix.
func MediumBonsaiIndented(prefix string) string {
	return Indent(MediumBonsai(), prefix)
}

// LargeBonsaiIndented returns a large bonsai indented with the given prefix.
func LargeBonsaiIndented(prefix string) string {
	return Indent(LargeBonsai(), prefix)
}

// SmallBonsaiString is a convenience wrapper that returns the small bonsai
// with standard 2-space indentation, ready for fmt.Print.
func SmallBonsaiString() string {
	return Indent(SmallBonsai(), "  ")
}

// MediumBonsaiString returns the medium bonsai ready for lipgloss panel embedding.
func MediumBonsaiString() string {
	return MediumBonsai()
}

// LargeBonsaiString returns the large bonsai with standard 2-space indentation.
func LargeBonsaiString() string {
	return Indent(LargeBonsai(), "  ")
}

