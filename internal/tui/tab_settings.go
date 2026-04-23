package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/config"
)

type settingsTab struct {
	cfg     *config.Config
	devices []capture.Device
	cursor  int

	focus   settingsFocus
	portIn  textinput.Model
	connIn  textinput.Model
	onSave  func() tea.Cmd
	loadErr error
	width   int
	height  int
}

type settingsFocus uint8

const (
	focusDevice settingsFocus = iota
	focusPort
	focusConn
)

func newSettingsTab(cfg *config.Config, onSave func() tea.Cmd) *settingsTab {
	port := textinput.New()
	port.Placeholder = "5555"
	port.CharLimit = 5
	port.Width = 8
	port.SetValue(strconv.Itoa(int(cfg.ServerPort)))

	conn := textinput.New()
	conn.Placeholder = "connection server hostname or IP"
	conn.CharLimit = 256
	conn.Width = 64
	conn.SetValue(cfg.ConnectionServer)

	t := &settingsTab{
		cfg:    cfg,
		portIn: port,
		connIn: conn,
		onSave: onSave,
	}
	if devs, err := capture.Devices(); err != nil {
		t.loadErr = err
	} else {
		t.devices = devs
		for i, d := range devs {
			if d.Name == cfg.Device {
				t.cursor = i
				break
			}
		}
	}
	return t
}

func (t *settingsTab) SetSize(w, h int) {
	t.width = w
	t.height = h
}

func (t *settingsTab) OnEnter() {}
func (t *settingsTab) OnLeave() {
	t.portIn.Blur()
	t.connIn.Blur()
	t.focus = focusDevice
}

func (t *settingsTab) Update(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(tea.MouseMsg); ok {
		if m.Action != tea.MouseActionPress {
			return nil, false
		}
		if t.focus != focusDevice {
			return nil, false
		}
		switch m.Button {
		case tea.MouseButtonWheelUp:
			if t.cursor > 0 {
				t.cursor--
			}
			return nil, true
		case tea.MouseButtonWheelDown:
			if t.cursor < len(t.devices)-1 {
				t.cursor++
			}
			return nil, true
		}
		return nil, false
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	if t.portIn.Focused() {
		switch key.Type {
		case tea.KeyEsc, tea.KeyEnter:
			if v, err := strconv.Atoi(strings.TrimSpace(t.portIn.Value())); err == nil && v > 0 && v < 65536 {
				t.cfg.ServerPort = uint16(v)
			}
			t.portIn.Blur()
			t.focus = focusDevice
			if key.Type == tea.KeyEnter && t.onSave != nil {
				return t.onSave(), true
			}
			return nil, true
		}
		var cmd tea.Cmd
		t.portIn, cmd = t.portIn.Update(msg)
		return cmd, true
	}
	if t.connIn.Focused() {
		switch key.Type {
		case tea.KeyEsc, tea.KeyEnter:
			t.cfg.ConnectionServer = strings.TrimSpace(t.connIn.Value())
			t.connIn.Blur()
			t.focus = focusDevice
			if key.Type == tea.KeyEnter && t.onSave != nil {
				return t.onSave(), true
			}
			return nil, true
		}
		var cmd tea.Cmd
		t.connIn, cmd = t.connIn.Update(msg)
		return cmd, true
	}

	switch key.String() {
	case "up", "k":
		if t.focus == focusDevice && t.cursor > 0 {
			t.cursor--
		}
		return nil, true
	case "down", "j":
		if t.focus == focusDevice && t.cursor < len(t.devices)-1 {
			t.cursor++
		}
		return nil, true
	case "enter":
		if t.focus == focusDevice && t.cursor >= 0 && t.cursor < len(t.devices) {
			t.cfg.Device = t.devices[t.cursor].Name
			if t.onSave != nil {
				return t.onSave(), true
			}
		}
		return nil, true
	case "p":
		t.focus = focusPort
		t.portIn.Focus()
		return textinput.Blink, true
	case "c":
		t.focus = focusConn
		t.connIn.Focus()
		return textinput.Blink, true
	}
	return nil, false
}

func (t *settingsTab) AnyInputFocused() bool {
	return t.portIn.Focused() || t.connIn.Focused()
}

func (t *settingsTab) View() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Settings"))
	b.WriteString("\n\n")

	b.WriteString(headingStyle.Render("Network device"))
	b.WriteString("\n")
	if t.loadErr != nil {
		b.WriteString(statusErr.Render("error: " + t.loadErr.Error()))
		b.WriteString("\n")
	}
	if len(t.devices) == 0 {
		b.WriteString(helpStyle.Render("(no devices found — on Windows install Npcap)"))
		b.WriteString("\n")
	}
	max := t.height - 18
	if max < 4 {
		max = 4
	}
	start := 0
	if t.cursor >= max {
		start = t.cursor - max + 1
	}
	end := start + max
	if end > len(t.devices) {
		end = len(t.devices)
	}
	for i := start; i < end; i++ {
		d := t.devices[i]
		label := d.Name
		if d.Description != "" {
			label = fmt.Sprintf("%s  (%s)", d.Name, d.Description)
		}
		marker := "  "
		if d.Name == t.cfg.Device {
			marker = "* "
		}
		line := marker + label
		if i == t.cursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("238")).
				Foreground(lipgloss.Color("230")).
				Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(headingStyle.Render("Server port"))
	b.WriteString("     ")
	b.WriteString(t.portIn.View())
	b.WriteString("\n")
	b.WriteString(headingStyle.Render("Connection server"))
	b.WriteString(" ")
	b.WriteString(t.connIn.View())

	b.WriteString("\n\n")
	if n := len(t.cfg.GameServerIPs); n > 0 {
		b.WriteString(helpStyle.Render(fmt.Sprintf("Discovered game server IPs (%d): ", n)))
		b.WriteString(strings.Join(t.cfg.GameServerIPs, ", "))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k=move  Enter=select/save  p=edit port  c=edit connection server"))
	return b.String()
}
