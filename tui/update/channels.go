// Package update bridges the engine's AppChannels to Bubbletea Msg types.
package update

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/atheory-ai/context-engine/internal/core"
)

// ChannelReader polls the engine's AppChannels and converts emissions
// to Bubbletea Msg types. Poll-based (not blocking goroutines) to keep
// Bubbletea's single-threaded model intact.
type ChannelReader struct {
	ch *core.AppChannels
}

// NewChannelReader creates a ChannelReader for the given AppChannels.
func NewChannelReader(ch *core.AppChannels) *ChannelReader {
	return &ChannelReader{ch: ch}
}

// PollCmd returns a Bubbletea command that drains all pending channel
// messages in a single non-blocking pass.
func (r *ChannelReader) PollCmd() tea.Cmd {
	return func() tea.Msg {
		return r.poll()
	}
}

// BatchMsg carries multiple messages from a single poll cycle.
type BatchMsg struct {
	Messages []tea.Msg
}

func (r *ChannelReader) poll() tea.Msg {
	var messages []tea.Msg

	for {
		select {
		case e, ok := <-r.ch.Thinking:
			if ok {
				messages = append(messages, ThinkingMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Action:
			if ok {
				messages = append(messages, ActionMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Message:
			if ok {
				messages = append(messages, MessageMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Warning:
			if ok {
				messages = append(messages, WarningMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Error:
			if ok {
				messages = append(messages, ErrorMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Cost:
			if ok {
				messages = append(messages, CostMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.System:
			if ok {
				messages = append(messages, SystemMsg{Emission: e})
				continue
			}
		case e, ok := <-r.ch.Progress:
			if ok {
				messages = append(messages, ProgressMsg{Emission: e})
				continue
			}
		default:
			if len(messages) == 0 {
				return nil
			}
			return BatchMsg{Messages: messages}
		}
	}
}
