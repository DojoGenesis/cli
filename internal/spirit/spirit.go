// Package spirit implements the Dojo Spirit engagement system:
// belt ranks, XP progression, achievements, daily streaks, and sensei koans.
package spirit

import (
	"math/rand"
	"sort"
	"time"
)

// ─── Belt Definitions ─────────────────────────────────────────────────────────

// Belt represents a dojo rank.
type Belt struct {
	Rank      int    // 0-7
	Name      string // "White", "Yellow", …
	Title     string // "Novice", "Apprentice", …
	Color     string // HEX for display
	Threshold int    // XP required to reach this belt
}

// Belts is the ordered belt ladder from White to Black.
var Belts = [8]Belt{
	{0, "White", "Novice", "#e8e8e8", 0},
	{1, "Yellow", "Apprentice", "#ffd166", 1000},
	{2, "Orange", "Initiate", "#f4a261", 3000},
	{3, "Green", "Practitioner", "#7fb88c", 6000},
	{4, "Blue", "Adept", "#457b9d", 10000},
	{5, "Purple", "Sage", "#9b59b6", 15000},
	{6, "Brown", "Master", "#8B6914", 25000},
	{7, "Black", "Grandmaster", "#1a1a2e", 50000},
}

// CurrentBelt returns the highest belt earned at the given XP.
func CurrentBelt(xp int) Belt {
	belt := Belts[0]
	for _, b := range Belts {
		if xp >= b.Threshold {
			belt = b
		}
	}
	return belt
}

// NextBelt returns the next belt and XP remaining to reach it.
// Returns nil if the practitioner already holds the Black belt.
func NextBelt(xp int) (*Belt, int) {
	for _, b := range Belts {
		if xp < b.Threshold {
			return &b, b.Threshold - xp
		}
	}
	return nil, 0
}

// ProgressPercent returns 0.0–1.0 progress from current belt to next.
// Returns 1.0 if at Black belt.
func ProgressPercent(xp int) float64 {
	cur := CurrentBelt(xp)
	next, _ := NextBelt(xp)
	if next == nil {
		return 1.0
	}
	span := float64(next.Threshold - cur.Threshold)
	if span == 0 {
		return 1.0
	}
	return float64(xp-cur.Threshold) / span
}

// ProgressBar renders an ASCII progress bar of the given width.
func ProgressBar(xp int, width int) string {
	pct := ProgressPercent(xp)
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	bar := make([]byte, width)
	for i := range bar {
		if i < filled {
			bar[i] = '#'
		} else {
			bar[i] = '-'
		}
	}
	return string(bar)
}

// ─── XP Engine ────────────────────────────────────────────────────────────────

// SpiritState is the persistent data stored inside state.json.
type SpiritState struct {
	XP              int               `json:"xp"`
	TotalSessions   int               `json:"total_sessions"`
	TotalCommands   int               `json:"total_commands"`
	TotalAgents     int               `json:"total_agents"`
	TotalSkills     int               `json:"total_skills"`
	TotalArtifacts  int               `json:"total_artifacts"`
	TotalProjects   int               `json:"total_projects"`
	TotalPractice   int               `json:"total_practice"`
	TotalSeeds      int               `json:"total_seeds"`
	TotalPlugins    int               `json:"total_plugins"`
	TotalGuides     int               `json:"total_guides"`
	StreakDays      int               `json:"streak_days"`
	LastActiveDate  string            `json:"last_active_date"`      // YYYY-MM-DD
	StreakBonusDate string            `json:"streak_bonus_date"`     // YYYY-MM-DD of last streak bonus
	MemberSince     string            `json:"member_since,omitempty"`
	Unlocked        map[string]string `json:"unlocked,omitempty"` // achievement_id -> RFC3339
	SessionStart    string            `json:"session_start,omitempty"`
	NightOwlSeen    bool              `json:"night_owl_seen,omitempty"`
	EarlyBirdSeen   bool              `json:"early_bird_seen,omitempty"`
}

// xpTable maps action names to XP values.
var xpTable = map[string]int{
	"session_start":        25,
	"command_run":          5,
	"chat_message":         5,
	"agent_dispatched":     50,
	"skill_invoked":        30,
	"artifact_saved":       20,
	"project_created":      100,
	"practice_completed":   40,
	"seed_planted":         15,
	"plugin_installed":     75,
	"model_changed":        10,
	"phase_advanced":       60,
	"belt_promotion":       200,
	"guide_step_completed": 15,
	"guide_completed":      75,
}

// XPForAction returns the XP value for a named action. Returns 0 for unknown actions.
func XPForAction(action string) int {
	return xpTable[action]
}

// AwardXP adds XP to the spirit state. Returns true and the new belt if a
// belt promotion occurred.
func AwardXP(s *SpiritState, amount int) (beltedUp bool, newBelt Belt) {
	if amount <= 0 {
		return false, CurrentBelt(s.XP)
	}
	oldBelt := CurrentBelt(s.XP)
	s.XP += amount
	newB := CurrentBelt(s.XP)
	if newB.Rank > oldBelt.Rank {
		return true, newB
	}
	return false, newB
}

// ─── Streak System ────────────────────────────────────────────────────────────

// UpdateStreak updates the streak state and returns bonus XP earned.
// Call once per session start. Uses a 48-hour grace window.
func UpdateStreak(s *SpiritState, now time.Time) int {
	today := now.Format("2006-01-02")

	// Already got streak bonus today
	if s.StreakBonusDate == today {
		return 0
	}

	if s.LastActiveDate == "" {
		// First ever session
		s.StreakDays = 1
		s.LastActiveDate = today
		s.StreakBonusDate = today
		return 0 // No bonus on first day
	}

	lastActive, err := time.Parse("2006-01-02", s.LastActiveDate)
	if err != nil {
		s.StreakDays = 1
		s.LastActiveDate = today
		s.StreakBonusDate = today
		return 0
	}

	daysSince := int(now.Sub(lastActive).Hours() / 24)

	switch daysSince {
	case 0:
		// Same day — no new streak increment, but award bonus if not yet today
		// (shouldn't happen because of StreakBonusDate check above)
		return 0
	case 1:
		// Consecutive day — streak continues
		s.StreakDays++
	case 2:
		// Grace window (48h) — streak continues
		s.StreakDays++
	default:
		// Streak broken
		s.StreakDays = 1
	}

	s.LastActiveDate = today
	s.StreakBonusDate = today

	// Bonus: streak_days * 5, max 150
	bonus := s.StreakDays * 5
	if bonus > 150 {
		bonus = 150
	}
	return bonus
}

// ─── Achievements ─────────────────────────────────────────────────────────────

// Achievement represents an unlockable badge.
type Achievement struct {
	ID          string
	Name        string
	Description string
	Icon        string
	XPReward    int
	check       func(s *SpiritState) bool
}

// AllAchievements returns the full list of unlockable achievements.
func AllAchievements() []Achievement {
	return []Achievement{
		{"first_steps", "First Steps", "Complete your first session", "*", 100,
			func(s *SpiritState) bool { return s.TotalSessions >= 1 }},
		{"green_thumb", "Green Thumb", "Plant 10 memory seeds", "@", 200,
			func(s *SpiritState) bool { return s.TotalSeeds >= 10 }},
		{"commander", "Commander", "Run 100 commands", "!", 300,
			func(s *SpiritState) bool { return s.TotalCommands >= 100 }},
		{"agent_smith", "Agent Smith", "Dispatch 10 agents", "%", 250,
			func(s *SpiritState) bool { return s.TotalAgents >= 10 }},
		{"plugin_collector", "Plugin Collector", "Install 5 plugins", "+", 200,
			func(s *SpiritState) bool { return s.TotalPlugins >= 5 }},
		{"night_owl", "Night Owl", "Start a session between midnight and 5am", "~", 150,
			func(s *SpiritState) bool { return s.NightOwlSeen }},
		{"early_bird", "Early Bird", "Start a session between 5am and 7am", "^", 150,
			func(s *SpiritState) bool { return s.EarlyBirdSeen }},
		{"marathon", "Marathon", "Run a session longer than 2 hours", "#", 300,
			func(s *SpiritState) bool {
				if s.SessionStart == "" {
					return false
				}
				start, err := time.Parse(time.RFC3339, s.SessionStart)
				if err != nil {
					return false
				}
				return time.Since(start) > 2*time.Hour
			}},
		{"streak_master", "Streak Master", "Maintain a 7-day streak", "=", 250,
			func(s *SpiritState) bool { return s.StreakDays >= 7 }},
		{"philosopher", "Philosopher", "Complete /practice 10 times", "&", 200,
			func(s *SpiritState) bool { return s.TotalPractice >= 10 }},
		{"first_guide", "First Guide", "Complete your first guided tutorial", ">", 100,
			func(s *SpiritState) bool { return s.TotalGuides >= 1 }},
		{"guide_explorer", "Guide Explorer", "Complete 5 guides", ">>", 250,
			func(s *SpiritState) bool { return s.TotalGuides >= 5 }},
		{"guide_master", "Guide Master", "Complete all 17 built-in guides", ">>>", 800,
			func(s *SpiritState) bool { return s.TotalGuides >= 17 }},
	}
}

// CheckAchievements evaluates all achievements and returns newly unlocked ones.
// Newly unlocked achievements are recorded in s.Unlocked.
func CheckAchievements(s *SpiritState, now time.Time) []Achievement {
	if s.Unlocked == nil {
		s.Unlocked = make(map[string]string)
	}
	var newly []Achievement
	for _, a := range AllAchievements() {
		if _, done := s.Unlocked[a.ID]; done {
			continue
		}
		if a.check(s) {
			s.Unlocked[a.ID] = now.UTC().Format(time.RFC3339)
			newly = append(newly, a)
		}
	}
	return newly
}

// UnlockedAchievements returns all unlocked achievements sorted by unlock time (newest first).
func UnlockedAchievements(s *SpiritState) []Achievement {
	all := AllAchievements()
	byID := make(map[string]Achievement, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}

	type unlocked struct {
		a  Achievement
		ts string
	}
	var list []unlocked
	for id, ts := range s.Unlocked {
		if a, ok := byID[id]; ok {
			list = append(list, unlocked{a, ts})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ts > list[j].ts })

	result := make([]Achievement, len(list))
	for i, u := range list {
		result[i] = u.a
	}
	return result
}

// ─── Sensei Koans ─────────────────────────────────────────────────────────────

// koans maps belt rank (0-7) to wisdom quotes.
var koans = map[int][]string{
	0: { // White — Novice
		"The journey of a thousand commits begins with a single line.",
		"Every master was once a beginner who refused to give up.",
		"An empty terminal is a canvas of infinite possibility.",
		"To learn, first admit what you do not know.",
		"The first step is always the hardest — and the most important.",
	},
	1: { // Yellow — Apprentice
		"Read the error message. Then read it again.",
		"Simple code that works beats clever code that confuses.",
		"The best tool is the one you understand.",
		"Naming is the first act of understanding.",
		"A question asked is a lesson half-learned.",
		"Trust the compiler — it has seen more bugs than you.",
	},
	2: { // Orange — Initiate
		"Duplication is far cheaper than the wrong abstraction.",
		"Code is read far more often than it is written.",
		"If you cannot explain it simply, you do not understand it well enough.",
		"The test that catches a bug before production is worth a hundred that don't.",
		"Write code for the next person — they may be you in six months.",
		"Delete code with confidence. Version control remembers what you forget.",
	},
	3: { // Green — Practitioner
		"Good code reads like well-written prose — intent clear, structure invisible.",
		"The best refactor is the one you don't have to explain.",
		"Ship small, learn fast, correct course.",
		"Architecture is the art of drawing lines that are easy to cross later.",
		"Every system grows until it needs a message queue.",
		"Measure twice, deploy once.",
	},
	4: { // Blue — Adept
		"The system is never finished. It is only at a resting point.",
		"Complexity is a debt with compound interest.",
		"The fastest code is the code that never runs.",
		"Observability is not optional — it is the foundation of trust.",
		"An interface is a promise. Break it carefully.",
		"When two systems disagree, the database is usually right.",
	},
	5: { // Purple — Sage
		"The hardest bugs are the ones where the code does exactly what you wrote.",
		"Wisdom is knowing when not to optimize.",
		"A well-placed log statement is worth a thousand debugger sessions.",
		"The team that ships together, debugs together.",
		"Simplicity on the other side of complexity is worth the journey.",
		"Legacy code is just code that makes money.",
	},
	6: { // Brown — Master
		"To master the tool, forget the tool. Build with intention.",
		"The senior engineer's job is to make the junior engineer's job possible.",
		"A system with no constraints is a system with no users.",
		"The art of engineering is the art of trade-offs made visible.",
		"Perfection is not when there is nothing more to add, but when there is nothing left to take away.",
		"The strongest architectures bend without breaking.",
	},
	7: { // Black — Grandmaster
		"The expert has failed more times than the beginner has tried.",
		"True mastery is knowing when not to code.",
		"In the beginner's mind there are many possibilities. In the expert's mind there are few — and they are the right ones.",
		"The dojo is not the building. The dojo is the practice.",
		"When the code and the comment disagree, both are probably wrong.",
		"To ship is human. To ship reliably is divine.",
		"After enlightenment, the laundry. After deployment, the monitoring.",
		"The empty terminal awaits your return. Rest well, practitioner.",
	},
}

// RandomKoan returns a random koan from the given belt rank or below.
func RandomKoan(beltRank int, now time.Time) string {
	var pool []string
	for rank := 0; rank <= beltRank && rank <= 7; rank++ {
		pool = append(pool, koans[rank]...)
	}
	if len(pool) == 0 {
		return "The path is quiet today."
	}
	//nolint:gosec // math/rand is fine for koan selection
	rng := rand.New(rand.NewSource(now.UnixNano()))
	return pool[rng.Intn(len(pool))]
}

// KoanCount returns the total number of koans accessible at the given belt rank.
func KoanCount(beltRank int) int {
	count := 0
	for rank := 0; rank <= beltRank && rank <= 7; rank++ {
		count += len(koans[rank])
	}
	return count
}

// TotalKoans returns the total number of koans across all belt levels.
func TotalKoans() int {
	count := 0
	for _, ks := range koans {
		count += len(ks)
	}
	return count
}

// ─── Belt Promotion Quotes ────────────────────────────────────────────────────

// beltQuotes maps belt rank to a promotion message.
var beltQuotes = map[int]string{
	1: "The path rewards those who begin.",
	2: "Your persistence is becoming visible.",
	3: "Practice has become part of your rhythm.",
	4: "You see patterns where others see chaos.",
	5: "Your wisdom guides those who follow.",
	6: "Few reach this height. Fewer still remain.",
	7: "The circle is complete. The practice continues.",
}

// BeltQuote returns the promotion quote for a belt rank.
func BeltQuote(rank int) string {
	if q, ok := beltQuotes[rank]; ok {
		return q
	}
	return "The journey continues."
}
