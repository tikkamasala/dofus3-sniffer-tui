package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pathsTab is a simple list of filesystem paths with add/delete support.
// Used by the Mappings tab. The Protos tab has its own compound component
// since it needs two schemas with envelope-name inputs.
type pathsTab struct {
	title    string
	paths    []string
	cursor   int
	input    textinput.Model
	onChange func(paths []string) tea.Cmd
	width    int
	height   int
}

func newPathsTab(title string, initial []string, onChange func([]string) tea.Cmd) *pathsTab {
	ti := textinput.New()
	ti.Placeholder = "C:\\path\\to\\file — Enter to add"
	ti.Prompt = "> "
	ti.CharLimit = 4096
	paths := make([]string, len(initial))
	copy(paths, initial)
	return &pathsTab{
		title:    title,
		paths:    paths,
		input:    ti,
		onChange: onChange,
	}
}

func (t *pathsTab) SetSize(w, h int) {
	t.width = w
	t.height = h
	t.input.Width = w - 4
	if t.input.Width < 10 {
		t.input.Width = 10
	}
}

func (t *pathsTab) OnEnter() {}
func (t *pathsTab) OnLeave() { t.input.Blur() }

func (t *pathsTab) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(tea.MouseMsg); ok {
		if m.Action != tea.MouseActionPress {
			return nil, false
		}
		switch m.Button {
		case tea.MouseButtonWheelUp:
			if t.cursor > 0 {
				t.cursor--
			}
			return nil, true
		case tea.MouseButtonWheelDown:
			if t.cursor < len(t.paths)-1 {
				t.cursor++
			}
			return nil, true
		}
		return nil, false
	}
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return nil, false
	}
	if t.input.Focused() {
		switch keyMsg.Type {
		case tea.KeyEnter:
			val := strings.TrimSpace(t.input.Value())
			if val != "" {
				t.paths = append(t.paths, val)
				t.input.SetValue("")
				if t.onChange != nil {
					return t.onChange(t.paths), true
				}
			}
			return nil, true
		case tea.KeyEsc:
			t.input.Blur()
			return nil, true
		}
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		return cmd, true
	}
	switch keyMsg.String() {
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
		return nil, true
	case "down", "j":
		if t.cursor < len(t.paths)-1 {
			t.cursor++
		}
		return nil, true
	case "i", "a":
		t.input.Focus()
		return textinput.Blink, true
	case "d", "delete":
		if t.cursor >= 0 && t.cursor < len(t.paths) {
			t.paths = append(t.paths[:t.cursor], t.paths[t.cursor+1:]...)
			if t.cursor >= len(t.paths) && t.cursor > 0 {
				t.cursor--
			}
			if t.onChange != nil {
				return t.onChange(t.paths), true
			}
		}
		return nil, true
	}
	return nil, false
}

func (t *pathsTab) View() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render(t.title))
	b.WriteString("\n\n")
	if len(t.paths) == 0 {
		b.WriteString(helpStyle.Render("(no paths — press 'i' to add one)"))
	}
	for i, p := range t.paths {
		line := fmt.Sprintf("  %s", p)
		if i == t.cursor && !t.input.Focused() {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("238")).
				Foreground(lipgloss.Color("230")).
				Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(t.input.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("i/a=add  d/Del=delete  j/k=move  Enter=save  Esc=unfocus input"))
	return b.String()
}
