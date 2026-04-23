package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"sniffer-tui/internal/capture"
)

// StartCaptureBridge forwards CapturedMessages from the capture goroutine to
// the Bubble Tea program. It batches every 50ms or every 256 items, whichever
// comes first, to keep Update() from being called per-message under load.
func StartCaptureBridge(ctx context.Context, p *tea.Program, in <-chan capture.CapturedMessage) {
	go func() {
		const maxBatch = 256
		const flushEvery = 50 * time.Millisecond
		batch := make([]capture.CapturedMessage, 0, maxBatch)
		ticker := time.NewTicker(flushEvery)
		defer ticker.Stop()
		flush := func() {
			if len(batch) == 0 {
				return
			}
			out := make([]capture.CapturedMessage, len(batch))
			copy(out, batch)
			p.Send(BatchMessagesMsg(out))
			batch = batch[:0]
		}
		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case m, ok := <-in:
				if !ok {
					flush()
					return
				}
				batch = append(batch, m)
				if len(batch) >= maxBatch {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()
}

// StartDropTicker periodically reports the capture-drop counter to the TUI so
// it can surface "dropped N" on the status line.
func StartDropTicker(ctx context.Context, p *tea.Program, dropped func() uint64) {
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		var last uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				now := dropped()
				if now != last {
					p.Send(TickDropMsg{Dropped: now - last})
					last = now
				}
			}
		}
	}()
}
