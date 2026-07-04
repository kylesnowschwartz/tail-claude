package parser

// ToolStats aggregates per-tool usage over a session. The TUI's stats view
// (stats.go aggregateMessageStats) builds and sorts these by CallCount
// descending.
type ToolStats struct {
	Name            string
	CallCount       int
	TotalDurationMs int64
	ErrorCount      int
}
