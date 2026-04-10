// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"context"
	"sort"
	"time"

	"github.com/DojoGenesis/dojo-cli/internal/client"
)

// ─── Garden Context ─────────────────────────────────────────────────────────

// GardenContext holds garden/memory state fetched from the gateway at startup
// and updated inline from memory_retrieved SSE events.
type GardenContext struct {
	Loaded        bool
	TotalSeeds    int
	TopSeeds      []SeedSummary // top 5 by usage_count
	LastRetrieval int           // count from last memory_retrieved event

	// From /v1/garden/stats
	TotalCompressions int
	TotalTokensSaved  int
	ActiveTurns       int
}

// SeedSummary is a compact view of a seed for dashboard display.
type SeedSummary struct {
	Name       string
	UsageCount int
}

// FetchGardenContext queries the gateway for garden state.
// Returns a GardenContext or an empty one if the gateway is unreachable.
// Uses a 3-second timeout to avoid blocking startup.
func FetchGardenContext(gw *client.Client) GardenContext {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	gc := GardenContext{}

	// ── Fetch seeds ─────────────────────────────────────────────────
	seeds, err := gw.Seeds(ctx)
	if err != nil {
		return gc // empty, Loaded stays false
	}
	gc.TotalSeeds = len(seeds)

	// Sort by usage_count descending, take top 5.
	sort.Slice(seeds, func(i, j int) bool {
		return seeds[i].UsageCount > seeds[j].UsageCount
	})
	top := 5
	if len(seeds) < top {
		top = len(seeds)
	}
	gc.TopSeeds = make([]SeedSummary, top)
	for i := 0; i < top; i++ {
		gc.TopSeeds[i] = SeedSummary{
			Name:       seeds[i].Name,
			UsageCount: seeds[i].UsageCount,
		}
	}

	// ── Fetch garden stats ──────────────────────────────────────────
	stats, err := gw.GardenStats(ctx)
	if err == nil {
		if v, ok := stats["total_compressions"].(float64); ok {
			gc.TotalCompressions = int(v)
		}
		if v, ok := stats["total_tokens_saved"].(float64); ok {
			gc.TotalTokensSaved = int(v)
		}
		if ss, ok := stats["session_stats"].(map[string]interface{}); ok {
			if v, ok := ss["active_turns"].(float64); ok {
				gc.ActiveTurns = int(v)
			}
		}
	}

	gc.Loaded = true
	return gc
}

// HandleMemoryRetrieved updates garden context from a memory_retrieved SSE event.
func (g *GardenContext) HandleMemoryRetrieved(data map[string]any) {
	if v, ok := data["memories_found"].(float64); ok {
		g.LastRetrieval = int(v)
	}
}
