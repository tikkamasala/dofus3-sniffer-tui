package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sniffer-tui/internal/config"
)

// schemaSub holds the editable state for one schema (connection or game).
type schemaSub struct {
	title         string
	envelopeInput textinput.Model
	pathInput     textinput.Model
	paths         []string
	cursor        int

	// onChange is invoked whenever paths or envelope name change; callers use
	// this to persist config + trigger recompile.
	onChange func(s *schemaSub) tea.Cmd

	width, height int
}

func newSchemaSub(title string, initial config.SchemaConfig, onChange func(*schemaSub) tea.Cmd) *schemaSub {
	env := textinput.New()
	env.Placeholder = "Message"
	env.CharLimit = 128
	env.Width = 32
	env.SetValue(initial.EnvelopeMessageName)

	pi := textinput.New()
	pi.Placeholder = "C:\\path\\to\\file.proto — Enter to add"
	pi.Prompt = "> "
	pi.CharLimit = 4096

	paths := make([]string, len(initial.ProtoPaths))
	copy(paths, initial.ProtoPaths)

	return &schemaSub{
		title:         title,
		envelopeInput: env,
		pathInput:     pi,
		paths:         paths,
		onChange:      onChange,
	}
}

func (s *schemaSub) snapshot() config.SchemaConfig {
	name := strings.TrimSpace(s.envelopeInput.Value())
	if name == "" {
		name = "Message"
	}
	paths := make([]string, len(s.paths))
	copy(paths, s.paths)
	return config.SchemaConfig{ProtoPaths: paths, EnvelopeMessageName: name}
}

func (s *schemaSub) blurAll() {
	s.envelopeInput.Blur()
	s.pathInput.Blur()
}

func (s *schemaSub) focused() bool {
	return s.envelopeInput.Focused() || s.pathInput.Focused()
}

type protosTab struct {
	subs           [2]*schemaSub
	active         int            // 0 = connection, 1 = game
	globalOnUpdate func() tea.Cmd // no-op placeholder
	width          int
	height         int
}

func newProtosTab(cfg *config.Config, onChange func(which RegistryKind, s config.SchemaConfig) tea.Cmd) *protosTab {
	t := &protosTab{}
	t.subs[0] = newSchemaSub("Connection", cfg.Connection, func(s *schemaSub) tea.Cmd {
		cfg.Connection = s.snapshot()
		return onChange(RegConnection, cfg.Connection)
	})
	t.subs[1] = newSchemaSub("Game", cfg.Game, func(s *schemaSub) tea.Cmd {
		cfg.Game = s.snapshot()
		return onChange(RegGame, cfg.Game)
	})
	return t
}

func (t *protosTab) current() *schemaSub { return t.subs[t.active] }

func (t *protosTab) SetSize(w, h int) {
	t.width = w
	t.height = h
	pathInputW := w - 4
	if pathInputW < 10 {
		pathInputW = 10
	}
	for _, s := range t.subs {
		s.width = w
		s.height = h
		s.pathInput.Width = pathInputW
	}
}

func (t *protosTab) OnEnter() {}
func (t *protosTab) OnLeave() {
	for _, s := range t.subs {
		s.blurAll()
	}
}

// AnyInputFocused reports whether a text input in the active sub is absorbing
// keystrokes (needed by the root app to gate global shortcuts).
func (t *protosTab) AnyInputFocused() bool {
	return t.current().focused()
}

func (t *protosTab) Update(msg tea.Msg) (tea.Cmd, bool) {
	sub := t.current()

	if m, ok := msg.(tea.MouseMsg); ok {
		if m.Action != tea.MouseActionPress {
			return nil, false
		}
		switch m.Button {
		case tea.MouseButtonWheelUp:
			if sub.cursor > 0 {
				sub.cursor--
			}
			return nil, true
		case tea.MouseButtonWheelDown:
			if sub.cursor < len(sub.paths)-1 {
				sub.cursor++
			}
			return nil, true
		}
		return nil, false
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	// If either input is focused, it absorbs all keys until Esc/Enter.
	if sub.envelopeInput.Focused() {
		switch key.Type {
		case tea.KeyEsc:
			sub.envelopeInput.Blur()
			return nil, true
		case tea.KeyEnter:
			sub.envelopeInput.Blur()
			if sub.onChange != nil {
				return sub.onChange(sub), true
			}
			return nil, true
		}
		var cmd tea.Cmd
		sub.envelopeInput, cmd = sub.envelopeInput.Update(key)
		return cmd, true
	}
	if sub.pathInput.Focused() {
		switch key.Type {
		case tea.KeyEnter:
			val := strings.TrimSpace(sub.pathInput.Value())
			if val != "" {
				sub.paths = append(sub.paths, val)
				sub.pathInput.SetValue("")
				if sub.onChange != nil {
					return sub.onChange(sub), true
				}
			}
			return nil, true
		case tea.KeyEsc:
			sub.pathInput.Blur()
			return nil, true
		}
		var cmd tea.Cmd
		sub.pathInput, cmd = sub.pathInput.Update(key)
		return cmd, true
	}

	switch key.String() {
	case "s", "ctrl+right":
		t.active = (t.active + 1) % 2
		return nil, true
	case "ctrl+left":
		t.active = (t.active + 1) % 2 // only two, so same as forward
		return nil, true
	case "up", "k":
		if sub.cursor > 0 {
			sub.cursor--
		}
		return nil, true
	case "down", "j":
		if sub.cursor < len(sub.paths)-1 {
			sub.cursor++
		}
		return nil, true
	case "i", "a":
		sub.pathInput.Focus()
		return textinput.Blink, true
	case "e":
		sub.envelopeInput.Focus()
		return textinput.Blink, true
	case "d", "delete":
		if sub.cursor >= 0 && sub.cursor < len(sub.paths) {
			sub.paths = append(sub.paths[:sub.cursor], sub.paths[sub.cursor+1:]...)
			if sub.cursor >= len(sub.paths) && sub.cursor > 0 {
				sub.cursor--
			}
			if sub.onChange != nil {
				return sub.onChange(sub), true
			}
		}
		return nil, true
	}
	return nil, false
}

func (t *protosTab) View() string {
	var b strings.Builder

	// Mini tab bar for Connection / Game.
	b.WriteString(renderMiniTabs([]string{t.subs[0].title, t.subs[1].title}, t.active))
	b.WriteString("\n\n")

	sub := t.current()
	b.WriteString(headingStyle.Render(fmt.Sprintf("%s schema", sub.title)))
	b.WriteString("\n\n")
	b.WriteString("Envelope message name: ")
	b.WriteString(sub.envelopeInput.View())
	b.WriteString("\n\n")

	b.WriteString(headingStyle.Render(".proto file paths"))
	b.WriteString("\n")
	if len(sub.paths) == 0 {
		b.WriteString(helpStyle.Render("(no paths — press 'i' to add one)"))
		b.WriteString("\n")
	}
	for i, p := range sub.paths {
		line := "  " + p
		if i == sub.cursor && !sub.pathInput.Focused() && !sub.envelopeInput.Focused() {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("238")).
				Foreground(lipgloss.Color("230")).
				Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(sub.pathInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("s=switch schema  e=edit envelope  i/a=add  d/Del=delete  j/k=move  Enter=save  Esc=unfocus"))
	return b.String()
}

// renderMiniTabs draws a secondary tab bar inside another tab.
func renderMiniTabs(labels []string, active int) string {
	var parts []string
	for i, l := range labels {
		if i == active {
			parts = append(parts, tabActive.Render(l))
		} else {
			parts = append(parts, tabInactive.Render(l))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
