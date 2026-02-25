package core

// Emission is a unit of output from any cognitive loop node.
// All output flows through typed emissions, not direct I/O.
type Emission struct {
	RunID     RunID
	TurnID    TurnID
	LoopIndex int
	Source    string      // which node produced this (strategizer, tool:name, etc.)
	Channel   ChannelType
	Content   string
	Markdown  bool
	Metadata  map[string]any
}
