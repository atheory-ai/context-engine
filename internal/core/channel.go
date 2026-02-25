package core

// ChannelType identifies which output stream an emission belongs to.
type ChannelType string

const (
	ChanThinking ChannelType = "thinking"  // internal reasoning, dim in UI
	ChanAction   ChannelType = "action"    // tool activations, status
	ChanMessage  ChannelType = "message"   // final LLM speech, always markdown
	ChanDebug    ChannelType = "debug"     // --debug flag only
	ChanError    ChannelType = "error"     // errors
	ChanWarning  ChannelType = "warning"   // warnings
	ChanProgress ChannelType = "progress"  // progress bars, spinners
	ChanCoverage ChannelType = "coverage"  // structured coverage summary
	ChanCost     ChannelType = "cost"      // --show-cost flag only
	ChanSystem   ChannelType = "system"    // lifecycle events
)

// AppChannels is the centralized set of channels that flows through the
// entire cognitive loop. The runner creates it; all nodes write to it;
// the TUI/CLI pops from it.
type AppChannels struct {
	Thinking chan Emission
	Action   chan Emission
	Message  chan Emission
	Debug    chan Emission
	Error    chan Emission
	Warning  chan Emission
	Progress chan Emission
	Coverage chan Emission
	Cost     chan Emission
	System   chan Emission
}

// NewAppChannels creates AppChannels with appropriate buffer sizes.
// Buffer sizes are intentionally generous — the goal is that no cognitive
// loop node ever blocks on a channel write.
func NewAppChannels() AppChannels {
	return AppChannels{
		Thinking: make(chan Emission, 64),
		Action:   make(chan Emission, 32),
		Message:  make(chan Emission, 16),
		Debug:    make(chan Emission, 128),
		Error:    make(chan Emission, 16),
		Warning:  make(chan Emission, 16),
		Progress: make(chan Emission, 32),
		Coverage: make(chan Emission, 8),
		Cost:     make(chan Emission, 8),
		System:   make(chan Emission, 16),
	}
}

// Emit sends to the correct channel based on the emission's ChannelType.
// Non-blocking — if the channel is full, the emission is dropped.
func (c *AppChannels) Emit(e Emission) {
	switch e.Channel {
	case ChanThinking:
		select {
		case c.Thinking <- e:
		default:
		}
	case ChanAction:
		select {
		case c.Action <- e:
		default:
		}
	case ChanMessage:
		select {
		case c.Message <- e:
		default:
		}
	case ChanDebug:
		select {
		case c.Debug <- e:
		default:
		}
	case ChanError:
		select {
		case c.Error <- e:
		default:
		}
	case ChanWarning:
		select {
		case c.Warning <- e:
		default:
		}
	case ChanProgress:
		select {
		case c.Progress <- e:
		default:
		}
	case ChanCoverage:
		select {
		case c.Coverage <- e:
		default:
		}
	case ChanCost:
		select {
		case c.Cost <- e:
		default:
		}
	case ChanSystem:
		select {
		case c.System <- e:
		default:
		}
	}
}
