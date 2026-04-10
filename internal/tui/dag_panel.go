// Package tui provides Bubbletea-based terminal UI dashboards for the Dojo CLI.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── DAG Node State ────────────────────────────────────────────────────────

// DAGNodeState represents the execution state of a single DAG node.
type DAGNodeState string

const (
	NodePending DAGNodeState = "pending"
	NodeRunning DAGNodeState = "running"
	NodeSuccess DAGNodeState = "success"
	NodeFailed  DAGNodeState = "failed"
	NodeSkipped DAGNodeState = "skipped"
)

// ─── DAG Node ──────────────────────────────────────────────────────────────

// DAGNode is a single step in the orchestration DAG.
type DAGNode struct {
	ID       string
	ToolName string
	State    DAGNodeState
	Duration int64  // milliseconds
	Error    string
}

// ─── DAG State ─────────────────────────────────────────────────────────────

// DAGState tracks the live orchestration plan for the Pilot context panel.
type DAGState struct {
	Active     bool
	PlanID     string
	TaskID     string
	Nodes      []DAGNode            // ordered by creation
	NodeMap    map[string]*DAGNode  // node_id -> node (for fast updates)
	TotalNodes int
	EstCost    float64
	Replans    int
	Status     string // "running", "completed", "failed"
	DurationMs int64
}

// NewDAGState creates an empty, inactive DAGState.
func NewDAGState() *DAGState {
	return &DAGState{
		NodeMap: make(map[string]*DAGNode),
	}
}

// HandleEvent processes an orchestration SSE event and updates DAG state.
func (d *DAGState) HandleEvent(eventType string, data map[string]any) {
	switch eventType {

	case "orchestration_plan_created":
		d.Active = true
		d.PlanID = getStr(data, "plan_id")
		d.TaskID = getStr(data, "task_id")
		d.TotalNodes = int(getFloat(data, "node_count"))
		d.EstCost = getFloat(data, "estimated_cost")
		d.Status = "running"
		// Pre-populate nodes if the payload includes them.
		if nodesRaw, ok := data["nodes"].([]any); ok {
			for _, nr := range nodesRaw {
				nm, ok := nr.(map[string]any)
				if !ok {
					continue
				}
				id := getStr(nm, "node_id")
				tool := getStr(nm, "tool_name")
				if id == "" {
					continue
				}
				node := DAGNode{
					ID:       id,
					ToolName: tool,
					State:    NodePending,
				}
				d.Nodes = append(d.Nodes, node)
				d.NodeMap[id] = &d.Nodes[len(d.Nodes)-1]
			}
		}

	case "orchestration_node_start":
		nodeID := getStr(data, "node_id")
		tool := getStr(data, "tool_name")
		if existing, ok := d.NodeMap[nodeID]; ok {
			existing.State = NodeRunning
			if tool != "" {
				existing.ToolName = tool
			}
		} else if nodeID != "" {
			// Node not pre-populated — add dynamically.
			node := DAGNode{
				ID:       nodeID,
				ToolName: tool,
				State:    NodeRunning,
			}
			d.Nodes = append(d.Nodes, node)
			d.NodeMap[nodeID] = &d.Nodes[len(d.Nodes)-1]
		}

	case "orchestration_node_end":
		nodeID := getStr(data, "node_id")
		state := getStr(data, "state")
		if state == "" {
			state = getStr(data, "status")
		}
		dur := int64(getFloat(data, "duration_ms"))
		errMsg := getStr(data, "error")
		if existing, ok := d.NodeMap[nodeID]; ok {
			switch state {
			case "success":
				existing.State = NodeSuccess
			case "failed":
				existing.State = NodeFailed
			case "skipped":
				existing.State = NodeSkipped
			default:
				existing.State = NodeSuccess
			}
			existing.Duration = dur
			existing.Error = errMsg
		}

	case "orchestration_replanning":
		d.Replans++

	case "orchestration_complete":
		d.Status = "completed"
		d.DurationMs = int64(getFloat(data, "duration_ms"))

	case "orchestration_failed":
		d.Status = "failed"
	}
}

// ─── DAG Render ────────────────────────────────────────────────────────────

var (
	styleDAGTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAmber))

	styleDAGSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorGreen))

	styleDAGRunning = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorYellow))

	styleDAGFailed = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorRed))

	styleDAGPending = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle))

	styleDAGSkipped = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray))

	styleDAGInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCloudGray))
)

// Render produces the compact TUI representation of the DAG state.
// width and height constrain the output; nodes beyond height are truncated.
func (d *DAGState) Render(width, height int) string {
	if !d.Active {
		return ""
	}

	var sb strings.Builder

	// ── Header line ──
	planLabel := d.PlanID
	if len(planLabel) > 12 {
		planLabel = planLabel[:12]
	}
	headerLine := fmt.Sprintf("DAG: %s (%d nodes", planLabel, d.TotalNodes)
	if d.EstCost > 0 {
		headerLine += fmt.Sprintf(", est $%.4f", d.EstCost)
	}
	headerLine += ")"
	sb.WriteString(" " + styleDAGTitle.Render(headerLine) + "\n")

	// ── Node list ──
	// Reserve 2 lines for header + footer.
	maxNodes := height - 2
	if maxNodes < 1 {
		maxNodes = 1
	}

	visibleNodes := d.Nodes
	truncated := false
	if len(visibleNodes) > maxNodes {
		visibleNodes = visibleNodes[:maxNodes]
		truncated = true
	}

	for _, n := range visibleNodes {
		var icon string
		var styledLine string
		toolLabel := n.ToolName
		if toolLabel == "" {
			toolLabel = n.ID
		}
		// Truncate tool name to fit.
		maxToolW := width - 18
		if maxToolW < 8 {
			maxToolW = 8
		}
		if len(toolLabel) > maxToolW {
			toolLabel = toolLabel[:maxToolW-1] + "…"
		}

		switch n.State {
		case NodeSuccess:
			icon = styleDAGSuccess.Render("[ok]")
			durStr := styleDAGInfo.Render(fmt.Sprintf("%dms", n.Duration))
			styledLine = fmt.Sprintf(" %s %s  %s", icon, toolLabel, durStr)
		case NodeRunning:
			icon = styleDAGRunning.Render("[>>]")
			styledLine = fmt.Sprintf(" %s %s  %s", icon, toolLabel, styleDAGRunning.Render("..."))
		case NodeFailed:
			icon = styleDAGFailed.Render("[!!]")
			errSnip := n.Error
			if len(errSnip) > 30 {
				errSnip = errSnip[:29] + "…"
			}
			styledLine = fmt.Sprintf(" %s %s  %s", icon, toolLabel, styleDAGFailed.Render(errSnip))
		case NodeSkipped:
			icon = styleDAGSkipped.Render("[--]")
			styledLine = fmt.Sprintf(" %s %s  %s", icon, toolLabel, styleDAGSkipped.Render("skipped"))
		default: // pending
			icon = styleDAGPending.Render("[  ]")
			styledLine = fmt.Sprintf(" %s %s", icon, styleDAGPending.Render(toolLabel))
		}
		sb.WriteString(styledLine + "\n")
	}

	if truncated {
		remaining := len(d.Nodes) - maxNodes
		sb.WriteString(" " + styleDAGInfo.Render(fmt.Sprintf("  ... +%d more", remaining)) + "\n")
	}

	// ── Footer line ──
	var statusStyle lipgloss.Style
	switch d.Status {
	case "completed":
		statusStyle = styleDAGSuccess
	case "failed":
		statusStyle = styleDAGFailed
	default:
		statusStyle = styleDAGRunning
	}
	footer := fmt.Sprintf("Replans: %d | Status: %s", d.Replans, d.Status)
	if d.DurationMs > 0 {
		footer += fmt.Sprintf(" (%dms)", d.DurationMs)
	}
	sb.WriteString(" " + statusStyle.Render(footer))

	return sb.String()
}
