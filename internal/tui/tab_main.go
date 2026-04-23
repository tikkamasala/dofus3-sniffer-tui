package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/tui/components"
)

// DecodeFunc decodes an item's Any.value using the current registry.
// version should be the registry version at decode time; the caller compares
// against registry.Version() to invalidate stale results.
type DecodeFunc func(it *components.Item) tea.Cmd

type mainTab struct {
	list       *components.RingList
	filter     textinput.Model
	preview    viewport.Model
	decode     DecodeFunc
	lastSeq    uint64
	lastResult DecodedMsg
	width      int
	height     int
}

func newMainTab(list *components.RingList, decode DecodeFunc) *mainTab {
	fi := textinput.New()
	fi.Placeholder = "filter (substring)"
	fi.Prompt = "/ "
	fi.CharLimit = 256
	vp := viewport.New(40, 10)
	return &mainTab{
		list:    list,
		filter:  fi,
		preview: vp,
		decode:  decode,
	}
}

func (t *mainTab) SetSize(w, h int) {
	t.width = w
	t.height = h
	listW := w / 2
	if listW < 30 {
		listW = 30
	}
	previewW := w - listW - 1
	if previewW < 20 {
		previewW = 20
	}
	// Filter input eats 1 row at the top; ringlist fills the rest on the left.
	filterH := 1
	listH := h - filterH
	if listH < 3 {
		listH = 3
	}
	t.filter.Width = listW - 2
	if t.filter.Width < 8 {
		t.filter.Width = 8
	}
	t.list.SetSize(listW, listH)
	t.preview.Width = previewW
	t.preview.Height = h
}

func (t *mainTab) OnEnter() {}
func (t *mainTab) OnLeave() { t.filter.Blur() }

func (t *mainTab) Ingest(batch []capture.CapturedMessage) tea.Cmd {
	for _, m := range batch {
		t.list.Append(m)
	}
	return t.maybeDecodeSelection()
}

func (t *mainTab) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case DecodedMsg:
		if sel := t.list.Selected(); sel != nil && m.Seq == sel.Seq {
			t.lastResult = m
			t.applyPreview()
		}
		return nil, true
	case tea.MouseMsg:
		return t.handleMouse(m)
	case tea.KeyMsg:
		return t.handleKey(m)
	}
	return nil, false
}

func (t *mainTab) handleMouse(m tea.MouseMsg) (tea.Cmd, bool) {
	if m.Action != tea.MouseActionPress {
		return nil, false
	}
	listW := t.width / 2
	if listW < 30 {
		listW = 30
	}
	onLeftPane := m.X < listW
	switch m.Button {
	case tea.MouseButtonWheelUp:
		if onLeftPane {
			t.list.ScrollBy(-3)
		} else {
			t.preview.ScrollUp(3)
		}
		return nil, true
	case tea.MouseButtonWheelDown:
		if onLeftPane {
			t.list.ScrollBy(3)
		} else {
			t.preview.ScrollDown(3)
		}
		return nil, true
	}
	return nil, false
}

func (t *mainTab) handleKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if t.filter.Focused() {
		switch k.Type {
		case tea.KeyEsc, tea.KeyEnter:
			t.filter.Blur()
			return nil, true
		}
		prev := t.filter.Value()
		var cmd tea.Cmd
		t.filter, cmd = t.filter.Update(k)
		if t.filter.Value() != prev {
			t.list.SetFilter(t.filter.Value())
			if dc := t.maybeDecodeSelection(); dc != nil {
				return tea.Batch(cmd, dc), true
			}
		}
		return cmd, true
	}
	switch k.String() {
	case "/":
		t.filter.Focus()
		return textinput.Blink, true
	case "up", "k":
		t.list.MoveUp()
		return t.maybeDecodeSelection(), true
	case "down", "j":
		t.list.MoveDown()
		return t.maybeDecodeSelection(), true
	case "pgup":
		t.list.PageUp()
		return t.maybeDecodeSelection(), true
	case "pgdown":
		t.list.PageDown()
		return t.maybeDecodeSelection(), true
	case "home", "g":
		t.list.Home()
		return t.maybeDecodeSelection(), true
	case "end", "G":
		t.list.End()
		return t.maybeDecodeSelection(), true
	case "ctrl+u":
		t.preview.HalfViewUp()
		return nil, true
	case "ctrl+d":
		t.preview.HalfViewDown()
		return nil, true
	case "b":
		n := t.list.BlacklistVisible()
		dc := t.maybeDecodeSelection()
		st := sendStatus(StatusInfo, sprintf("Blacklisted %d TypeURLs (total %d)", n, t.list.BlacklistCount()))
		return tea.Batch(dc, st), true
	case "B":
		t.list.ClearBlacklist()
		dc := t.maybeDecodeSelection()
		st := sendStatus(StatusInfo, "Blacklist cleared")
		return tea.Batch(dc, st), true
	case "c":
		n := t.list.Clear()
		t.lastSeq = 0
		t.lastResult = DecodedMsg{}
		t.preview.SetContent("")
		return sendStatus(StatusInfo, sprintf("Cleared %d messages", n)), true
	}
	return nil, false
}

func (t *mainTab) maybeDecodeSelection() tea.Cmd {
	sel := t.list.Selected()
	if sel == nil {
		t.lastSeq = 0
		t.preview.SetContent("")
		return nil
	}
	if sel.Seq == t.lastSeq && (t.lastResult.Text != "" || t.lastResult.Err != nil) {
		return nil
	}
	t.lastSeq = sel.Seq
	if t.decode != nil {
		return t.decode(sel)
	}
	return nil
}

// Invalidate drops the memoized decode result (e.g. on registry reload) so the
// next selection refresh re-decodes under the new descriptors.
func (t *mainTab) Invalidate() tea.Cmd {
	t.lastResult = DecodedMsg{}
	t.lastSeq = 0
	return t.maybeDecodeSelection()
}

func (t *mainTab) applyPreview() {
	if t.lastResult.Err != nil {
		t.preview.SetContent(statusErr.Render("decode error: " + t.lastResult.Err.Error()))
		return
	}
	t.preview.SetContent(t.lastResult.Text)
}

func (t *mainTab) View() string {
	listW := t.width / 2
	if listW < 30 {
		listW = 30
	}
	left := lipgloss.JoinVertical(
		lipgloss.Left,
		t.filter.View(),
		t.list.View(),
	)
	leftStyled := lipgloss.NewStyle().Width(listW).Render(left)
	right := t.preview.View()
	div := paneDivider.Render(strings.Repeat("│", 1))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, div, right)
}
