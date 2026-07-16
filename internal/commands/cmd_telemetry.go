package commands

// cmd_telemetry.go — /telemetry command: query observability telemetry from the Dojo telemetry API.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	gcolor "github.com/gookit/color"
)

// ─── Telemetry API response types ──────────────────────────────────────────

// API response wrapper: GET /api/telemetry/sessions returns {"sessions": [...]}
type telemetrySessionsResponse struct {
	Sessions []telemetrySession `json:"sessions"`
}

type telemetrySession struct {
	ID             string  `json:"id"`
	StartedAt      int64   `json:"started_at"`
	EndedAt        *int64  `json:"ended_at"`
	TotalCost      float64 `json:"total_cost"`
	TotalTokens    int64   `json:"total_tokens"`
	TotalToolCalls int     `json:"total_tool_calls"`
	TotalErrors    int     `json:"total_errors"`
	EventCount     int     `json:"event_count"`
}

// API response wrapper: GET /api/telemetry/costs returns {costs, trend, summary}
type telemetryCostsResponse struct {
	Costs   []telemetryCostRow   `json:"costs"`
	Trend   []telemetryDailyRow  `json:"trend"`
	Summary telemetryCostSummary `json:"summary"`
}

type telemetryCostSummary struct {
	TotalCost  float64                `json:"total_cost"`
	ByProvider []telemetryProviderRow `json:"by_provider"`
}

type telemetryCostRow struct {
	ID        int     `json:"id"`
	SessionID string  `json:"session_id"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
	Timestamp int64   `json:"timestamp"`
}

type telemetryProviderRow struct {
	Provider     string  `json:"provider"`
	TotalCost    float64 `json:"total_cost"`
	TotalTokenIn int64   `json:"total_tokens_in"`
	TotalTokenOut int64  `json:"total_tokens_out"`
	Count        int     `json:"count"`
}

type telemetryDailyRow struct {
	Day        string  `json:"day"`
	TotalCost  float64 `json:"total_cost"`
	TotalTokens int64  `json:"total_tokens"`
}

// API response wrapper: GET /api/telemetry/tools returns {"tools": [...]}
type telemetryToolsResponse struct {
	Tools []telemetryToolRow `json:"tools"`
}

type telemetryToolRow struct {
	Name        string  `json:"name"`
	Calls       int     `json:"calls"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	SuccessRate float64 `json:"success_rate"` // already 0-100 from API
}

// ─── /telemetry ────────────────────────────────────────────────────────────

func (r *Registry) telemetryCmd() Command {
	return Command{
		Name:    "telemetry",
		Aliases: []string{"telem", "obs"},
		Usage:   "/telemetry <sessions|costs|tools|summary>",
		Short:   "Query observability telemetry from the Dojo telemetry API",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return telemetryUsage()
			}
			sub := strings.ToLower(args[0])
			switch sub {
			case "sessions", "sess":
				return telemetrySessions(ctx)
			case "costs", "cost":
				return telemetryCosts(ctx)
			case "tools", "tool":
				return telemetryTools(ctx)
			case "summary", "sum":
				return telemetrySummary(ctx)
			default:
				return telemetryUsage()
			}
		},
	}
}

func telemetryUsage() error {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  /telemetry — observability commands"))
	fmt.Println()
	fmt.Println()
	fmt.Printf("    %-36s", gcolor.HEX("#f4a261").Sprint("/telemetry sessions"))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("recent sessions with cost/token/error data"))
	fmt.Printf("    %-36s", gcolor.HEX("#f4a261").Sprint("/telemetry costs"))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("cost breakdown by provider + 7-day trend"))
	fmt.Printf("    %-36s", gcolor.HEX("#f4a261").Sprint("/telemetry tools"))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("tool call stats: count, latency, success rate"))
	fmt.Printf("    %-36s", gcolor.HEX("#f4a261").Sprint("/telemetry summary"))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("combined overview of all telemetry data"))
	fmt.Println()
	return nil
}

// ─── telemetry base URL ────────────────────────────────────────────────────

func telemetryBaseURL() string {
	if u := os.Getenv("DOJO_TELEMETRY_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://dojo-telemetry.trespiesdesign.workers.dev"
}

// ─── HTTP helper ───────────────────────────────────────────────────────────

func telemetryGet(ctx context.Context, path string) ([]byte, error) {
	url := telemetryBaseURL() + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telemetry API unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telemetry API returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// ─── /telemetry sessions ──────────────────────────────────────────────────

func telemetrySessions(ctx context.Context) error {
	data, err := telemetryGet(ctx, "/api/telemetry/sessions?limit=20")
	if err != nil {
		return err
	}

	var resp telemetrySessionsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse sessions: %w", err)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Recent Sessions"))
	fmt.Println()
	fmt.Println()

	// Table header
	header := fmt.Sprintf("  %-14s %-14s %10s %12s %10s %7s",
		"Session ID", "Started", "Cost", "Tokens", "Tools", "Errors")
	fmt.Println(gcolor.HEX("#94a3b8").Sprint(header))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + strings.Repeat("─", 73)))

	for _, s := range resp.Sessions {
		sid := truncate(s.ID, 12)
		started := fmtUnixAgo(s.StartedAt)
		errStr := fmt.Sprintf("%d", s.TotalErrors)
		if s.TotalErrors > 0 {
			errStr = gcolor.HEX("#e63946").Sprintf("%d", s.TotalErrors)
		}
		fmt.Printf("  %-14s %-14s %10s %12s %10d %7s\n",
			gcolor.HEX("#f4a261").Sprint(sid),
			started,
			fmt.Sprintf("$%.4f", s.TotalCost),
			fmtTokens(s.TotalTokens),
			s.TotalToolCalls,
			errStr,
		)
	}

	if len(resp.Sessions) == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No sessions found."))
	}
	fmt.Println()
	return nil
}

// ─── /telemetry costs ─────────────────────────────────────────────────────

func telemetryCosts(ctx context.Context) error {
	data, err := telemetryGet(ctx, "/api/telemetry/costs?range=7d")
	if err != nil {
		return err
	}

	var costs telemetryCostsResponse
	if err := json.Unmarshal(data, &costs); err != nil {
		return fmt.Errorf("parse costs: %w", err)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Cost Summary (7d)"))
	fmt.Println()
	fmt.Println()

	// Compute total tokens from provider breakdown
	var totalTokens int64
	for _, p := range costs.Summary.ByProvider {
		totalTokens += p.TotalTokenIn + p.TotalTokenOut
	}

	printKV("total cost", fmt.Sprintf("$%.4f", costs.Summary.TotalCost))
	printKV("total tokens", fmtTokens(totalTokens))
	fmt.Println()

	// Provider breakdown table
	if len(costs.Summary.ByProvider) > 0 {
		gcolor.Bold.Print(gcolor.HEX("#94a3b8").Sprint("  Cost by Provider"))
		fmt.Println()
		fmt.Println()
		header := fmt.Sprintf("  %-20s %12s %14s", "Provider", "Cost", "Tokens")
		fmt.Println(gcolor.HEX("#94a3b8").Sprint(header))
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + strings.Repeat("─", 48)))
		for _, p := range costs.Summary.ByProvider {
			fmt.Printf("  %-20s %12s %14s\n",
				gcolor.HEX("#f4a261").Sprint(p.Provider),
				fmt.Sprintf("$%.4f", p.TotalCost),
				fmtTokens(p.TotalTokenIn+p.TotalTokenOut),
			)
		}
		fmt.Println()
	}

	// ASCII bar chart for daily trend
	if len(costs.Trend) > 0 {
		gcolor.Bold.Print(gcolor.HEX("#94a3b8").Sprint("  7-Day Trend"))
		fmt.Println()
		fmt.Println()
		printCostChart(costs.Trend)
		fmt.Println()
	}

	return nil
}

// ─── /telemetry tools ─────────────────────────────────────────────────────

func telemetryTools(ctx context.Context) error {
	data, err := telemetryGet(ctx, "/api/telemetry/tools?range=7d")
	if err != nil {
		return err
	}

	var resp telemetryToolsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse tools: %w", err)
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Tool Usage (7d)"))
	fmt.Println()
	fmt.Println()

	header := fmt.Sprintf("  %-28s %8s %14s %12s", "Tool", "Calls", "Avg Latency", "Success")
	fmt.Println(gcolor.HEX("#94a3b8").Sprint(header))
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  " + strings.Repeat("─", 66)))

	for _, t := range resp.Tools {
		name := truncate(t.Name, 26)
		// SuccessRate is already 0-100 from the API; do not multiply again
		rate := fmt.Sprintf("%.1f%%", t.SuccessRate)
		if t.SuccessRate < 90 {
			rate = gcolor.HEX("#e63946").Sprintf("%.1f%%", t.SuccessRate)
		}
		fmt.Printf("  %-28s %8d %12.1fms %12s\n",
			gcolor.HEX("#f4a261").Sprint(name),
			t.Calls,
			t.AvgLatency,
			rate,
		)
	}

	if len(resp.Tools) == 0 {
		fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No tool data found."))
	}
	fmt.Println()
	return nil
}

// ─── /telemetry summary ───────────────────────────────────────────────────

func telemetrySummary(ctx context.Context) error {
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Telemetry Summary"))
	fmt.Println()

	// Fetch all three endpoints
	sessData, sessErr := telemetryGet(ctx, "/api/telemetry/sessions?limit=5")
	costsData, costsErr := telemetryGet(ctx, "/api/telemetry/costs?range=7d")
	toolsData, toolsErr := telemetryGet(ctx, "/api/telemetry/tools?range=7d")

	// Cost overview
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#94a3b8").Sprint("  Costs (7d)"))
	fmt.Println()
	if costsErr != nil {
		fmt.Println(gcolor.HEX("#e63946").Sprintf("  error: %s", costsErr))
	} else {
		var costs telemetryCostsResponse
		if err := json.Unmarshal(costsData, &costs); err == nil {
			var totalTokens int64
			for _, p := range costs.Summary.ByProvider {
				totalTokens += p.TotalTokenIn + p.TotalTokenOut
			}
			printKV("total cost", fmt.Sprintf("$%.4f", costs.Summary.TotalCost))
			printKV("total tokens", fmtTokens(totalTokens))
			printKV("providers", fmt.Sprintf("%d", len(costs.Summary.ByProvider)))
		}
	}

	// Recent sessions
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#94a3b8").Sprint("  Recent Sessions"))
	fmt.Println()
	if sessErr != nil {
		fmt.Println(gcolor.HEX("#e63946").Sprintf("  error: %s", sessErr))
	} else {
		var sessResp telemetrySessionsResponse
		if err := json.Unmarshal(sessData, &sessResp); err == nil {
			for _, s := range sessResp.Sessions {
				sid := truncate(s.ID, 12)
				fmt.Printf("  %-14s %s  $%.4f  %s tokens\n",
					gcolor.HEX("#f4a261").Sprint(sid),
					fmtUnixAgo(s.StartedAt),
					s.TotalCost,
					fmtTokens(s.TotalTokens),
				)
			}
			if len(sessResp.Sessions) == 0 {
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No sessions found."))
			}
		}
	}

	// Top tools
	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#94a3b8").Sprint("  Top Tools (7d)"))
	fmt.Println()
	if toolsErr != nil {
		fmt.Println(gcolor.HEX("#e63946").Sprintf("  error: %s", toolsErr))
	} else {
		var toolsResp telemetryToolsResponse
		if err := json.Unmarshal(toolsData, &toolsResp); err == nil {
			limit := 5
			if len(toolsResp.Tools) < limit {
				limit = len(toolsResp.Tools)
			}
			for _, t := range toolsResp.Tools[:limit] {
				name := truncate(t.Name, 20)
				// SuccessRate is already 0-100 from the API
				fmt.Printf("  %-22s %5d calls  %.0fms avg  %.0f%% ok\n",
					gcolor.HEX("#f4a261").Sprint(name),
					t.Calls,
					t.AvgLatency,
					t.SuccessRate,
				)
			}
			if len(toolsResp.Tools) == 0 {
				fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No tool data found."))
			}
		}
	}

	fmt.Println()
	fmt.Println(gcolor.HEX("#94a3b8").Sprint("  Use /telemetry <sessions|costs|tools> for detail."))
	fmt.Println()
	return nil
}

// ─── Formatting helpers ───────────────────────────────────────────────────

// fmtTokens formats a token count with K/M suffixes.
func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// printCostChart renders a simple ASCII bar chart of daily costs.
func printCostChart(days []telemetryDailyRow) {
	if len(days) == 0 {
		return
	}

	// Find max cost for scaling
	maxCost := 0.0
	for _, d := range days {
		if d.TotalCost > maxCost {
			maxCost = d.TotalCost
		}
	}
	if maxCost == 0 {
		maxCost = 1 // avoid division by zero
	}

	barWidth := 30
	for _, d := range days {
		// Parse and format date label (API returns "YYYY-MM-DD" in the "day" field)
		label := d.Day
		if t, err := time.Parse("2006-01-02", d.Day); err == nil {
			label = t.Format("Jan 02")
		}

		filled := int(float64(barWidth) * (d.TotalCost / maxCost))
		if filled < 0 {
			filled = 0
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

		fmt.Printf("  %s  %s $%.4f\n",
			gcolor.HEX("#94a3b8").Sprintf("%-6s", label),
			gcolor.HEX("#7fb88c").Sprint(bar),
			d.TotalCost,
		)
	}
}
