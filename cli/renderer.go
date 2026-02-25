package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/atheory/context-engine/internal/core"
)

// ANSI color codes — used when noColor is false.
const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
)

// cliRenderer reads from AppChannels and writes formatted output to stdout.
type cliRenderer struct {
	ch        *core.AppChannels
	debug     bool
	showCost  bool
	noColor   bool
	showThink bool
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	writer    io.Writer
}

// newCLIRenderer creates a renderer. Stop()/Wait() must be called after the
// engine query completes to drain remaining buffered channel messages.
func newCLIRenderer(ch *core.AppChannels, debug, showCost, noColor, showThink bool) *cliRenderer {
	return &cliRenderer{
		ch:        ch,
		debug:     debug,
		showCost:  showCost,
		noColor:   noColor,
		showThink: showThink,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		writer:    os.Stdout,
	}
}

// Run reads from all channels and writes formatted output. Exits on Stop() or
// context cancellation.
func (r *cliRenderer) Run(ctx context.Context) {
	defer close(r.done)

	for {
		select {
		case e := <-r.ch.Thinking:
			r.handleThinking(e)
		case e := <-r.ch.Action:
			r.handleAction(e)
		case e := <-r.ch.System:
			r.handleSystem(e)
		case e := <-r.ch.Message:
			r.handleMessage(e)
		case e := <-r.ch.Warning:
			r.handleWarning(e)
		case e := <-r.ch.Error:
			r.handleError(e)
		case e := <-r.ch.Cost:
			r.handleCost(e)
		case e := <-r.ch.Debug:
			r.handleDebug(e)
		case <-r.stop:
			r.drainAll()
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop signals the renderer to drain remaining messages and exit.
// Safe to call multiple times.
func (r *cliRenderer) Stop() {
	r.once.Do(func() { close(r.stop) })
}

// Wait stops the renderer and blocks until it has drained all channels.
func (r *cliRenderer) Wait() {
	r.Stop()
	<-r.done
}

// drainAll empties all buffered channels after stop is signaled.
func (r *cliRenderer) drainAll() {
	r.drainChan(r.ch.System, r.handleSystem)
	r.drainChan(r.ch.Action, r.handleAction)
	r.drainChan(r.ch.Message, r.handleMessage)
	r.drainChan(r.ch.Warning, r.handleWarning)
	r.drainChan(r.ch.Error, r.handleError)
	r.drainChan(r.ch.Cost, r.handleCost)
	r.drainChan(r.ch.Debug, r.handleDebug)
	r.drainChan(r.ch.Thinking, r.handleThinking)
	r.drainChan(r.ch.Progress, func(_ core.Emission) {})
	r.drainChan(r.ch.Coverage, func(_ core.Emission) {})
}

func (r *cliRenderer) drainChan(ch chan core.Emission, fn func(core.Emission)) {
	for {
		select {
		case e := <-ch:
			fn(e)
		default:
			return
		}
	}
}

func (r *cliRenderer) handleThinking(e core.Emission) {
	if r.showThink || r.debug {
		r.printColored(ansiDim, "  ↳ "+e.Content)
	}
}

func (r *cliRenderer) handleAction(e core.Emission) {
	r.printColored(ansiCyan, "  • "+e.Content)
}

func (r *cliRenderer) handleSystem(e core.Emission) {
	r.printColored(ansiDim, "  · "+e.Content)
}

func (r *cliRenderer) handleMessage(e core.Emission) {
	fmt.Fprintln(r.writer)
	fmt.Fprintln(r.writer, e.Content)
}

func (r *cliRenderer) handleWarning(e core.Emission) {
	r.printColored(ansiYellow, "  ⚠ "+e.Content)
}

func (r *cliRenderer) handleError(e core.Emission) {
	r.printColored(ansiRed, "  ✗ "+e.Content)
}

func (r *cliRenderer) handleCost(e core.Emission) {
	if r.showCost {
		r.printColored(ansiDim, "  $ "+e.Content)
	}
}

func (r *cliRenderer) handleDebug(e core.Emission) {
	if r.debug {
		r.printColored(ansiDim, "  [debug] "+e.Content)
	}
}

func (r *cliRenderer) printColored(color, text string) {
	if r.noColor {
		fmt.Fprintln(r.writer, text)
	} else {
		fmt.Fprintf(r.writer, "%s%s%s\n", color, text, ansiReset)
	}
}
