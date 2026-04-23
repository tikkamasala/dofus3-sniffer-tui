package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/config"
	"sniffer-tui/internal/decode"
	"sniffer-tui/internal/protoreg"
	"sniffer-tui/internal/tui/components"
	"sniffer-tui/internal/watch"
)

type tabIdx int

const (
	tabMain tabIdx = iota
	tabProtos
	tabMappings
	tabSettings
	tabCount
)

var tabLabels = [tabCount]string{"Main", "Protos", "Mapping", "Settings"}

// Deps bundles the runtime dependencies of the root model. Provided by main.
type Deps struct {
	Cfg      *config.Config
	ConnReg  *protoreg.Registry
	GameReg  *protoreg.Registry
	Mappings *protoreg.Mappings
	Watcher  *watch.Watcher
	// StartCapture is called after the user saves settings that affect capture
	// (device, port, connection server, game IPs). Implementations tear down
	// any prior session first.
	StartCapture  func() error
	Dropped       func() uint64
	BytesReceived func() uint64
}

type App struct {
	deps Deps

	active tabIdx
	width  int
	height int

	main     *mainTab
	protos   *protosTab
	mappings *pathsTab
	settings *settingsTab

	status      string
	statusLevel StatusLevel
	statusAt    time.Time

	droppedTotal uint64
	decodeCtx    context.Context
	decodeCancel context.CancelFunc
}

func NewApp(deps Deps) *App {
	a := &App{deps: deps}
	a.decodeCtx, a.decodeCancel = context.WithCancel(context.Background())

	list := components.NewRingList(10_000, deps.Mappings.Friendly)
	a.main = newMainTab(list, a.decodeSelection)

	a.protos = newProtosTab(deps.Cfg, func(which RegistryKind, _ config.SchemaConfig) tea.Cmd {
		return tea.Batch(
			a.saveConfigCmd(),
			a.reloadWatcherCmd(watch.KindProto),
			a.reloadProtosCmd(which),
		)
	})
	a.mappings = newPathsTab("Mapping file paths", deps.Cfg.MappingPaths, func(paths []string) tea.Cmd {
		deps.Cfg.MappingPaths = paths
		return tea.Batch(
			a.saveConfigCmd(),
			a.reloadWatcherCmd(watch.KindMapping),
			a.reloadMappingsCmd(),
		)
	})
	a.settings = newSettingsTab(deps.Cfg, func() tea.Cmd {
		return tea.Batch(a.saveConfigCmd(), a.restartCaptureCmd())
	})

	// Start on Settings if no device is configured yet, else Main.
	if deps.Cfg.Device == "" {
		a.active = tabSettings
	} else {
		a.active = tabMain
	}
	return a
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.reloadProtosCmd(RegConnection),
		a.reloadProtosCmd(RegGame),
		a.reloadMappingsCmd(),
		a.reloadWatcherCmd(watch.KindProto),
		a.reloadWatcherCmd(watch.KindMapping),
	)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.layout()
		return a, nil

	case tea.KeyMsg:
		switch m.String() {
		case "ctrl+c", "ctrl+q":
			a.decodeCancel()
			return a, tea.Quit
		case "tab":
			a.switchTab((a.active + 1) % tabCount)
			return a, nil
		case "shift+tab":
			a.switchTab((a.active + tabCount - 1) % tabCount)
			return a, nil
		case "1":
			if !a.anyInputFocused() {
				a.switchTab(tabMain)
				return a, nil
			}
		case "2":
			if !a.anyInputFocused() {
				a.switchTab(tabProtos)
				return a, nil
			}
		case "3":
			if !a.anyInputFocused() {
				a.switchTab(tabMappings)
				return a, nil
			}
		case "4":
			if !a.anyInputFocused() {
				a.switchTab(tabSettings)
				return a, nil
			}
		case "q":
			if !a.anyInputFocused() {
				a.decodeCancel()
				return a, tea.Quit
			}
		}
		cmd, _ := a.dispatchKey(m)
		return a, cmd

	case BatchMessagesMsg:
		return a, a.main.Ingest([]capture.CapturedMessage(m))

	case ReloadMsg:
		switch m.Kind {
		case watch.KindProto:
			return a, tea.Batch(
				a.reloadProtosCmd(RegConnection),
				a.reloadProtosCmd(RegGame),
			)
		case watch.KindMapping:
			return a, a.reloadMappingsCmd()
		}
		return a, nil

	case RegistryReloadedMsg:
		which := m.Which.String()
		if m.Err != nil {
			a.setStatus(StatusErr, fmt.Sprintf("%s proto reload: %s", which, m.Err.Error()))
			if !m.HasEnv {
				return a, nil
			}
		} else {
			a.setStatus(StatusInfo, fmt.Sprintf("Loaded %s descriptors (v%d)", which, m.Version))
		}
		return a, a.main.Invalidate()

	case MappingsReloadedMsg:
		if m.Err != nil {
			a.setStatus(StatusErr, "mapping reload: "+m.Err.Error())
			return a, nil
		}
		a.setStatus(StatusInfo, "Mappings reloaded")
		return a, a.main.Invalidate()

	case DecodedMsg:
		cmd, _ := a.main.Update(m)
		return a, cmd

	case NewGameIPMsg:
		for _, existing := range a.deps.Cfg.GameServerIPs {
			if existing == m.IP {
				return a, nil
			}
		}
		a.deps.Cfg.GameServerIPs = append(a.deps.Cfg.GameServerIPs, m.IP)
		a.setStatus(StatusInfo, "Discovered game server IP: "+m.IP)
		return a, a.saveConfigCmd()

	case StatusMsg:
		a.setStatus(m.Level, m.Text)
		return a, nil

	case TickDropMsg:
		a.droppedTotal += m.Dropped
		a.setStatus(StatusWarn, fmt.Sprintf("capture dropped %d frames (total %d)", m.Dropped, a.droppedTotal))
		return a, nil
	}

	cmd, _ := a.dispatch(msg)
	return a, cmd
}

func (a *App) anyInputFocused() bool {
	switch a.active {
	case tabMain:
		return a.main.filter.Focused()
	case tabProtos:
		return a.protos.AnyInputFocused()
	case tabMappings:
		return a.mappings.input.Focused()
	case tabSettings:
		return a.settings.AnyInputFocused()
	}
	return false
}

func (a *App) dispatchKey(k tea.KeyMsg) (tea.Cmd, bool) {
	return a.dispatch(k)
}

func (a *App) dispatch(msg tea.Msg) (tea.Cmd, bool) {
	switch a.active {
	case tabMain:
		return a.main.Update(msg)
	case tabProtos:
		return a.protos.Update(msg)
	case tabMappings:
		return a.mappings.Update(msg)
	case tabSettings:
		return a.settings.Update(msg)
	}
	return nil, false
}

func (a *App) switchTab(to tabIdx) {
	if to == a.active {
		return
	}
	switch a.active {
	case tabMain:
		a.main.OnLeave()
	case tabProtos:
		a.protos.OnLeave()
	case tabMappings:
		a.mappings.OnLeave()
	case tabSettings:
		a.settings.OnLeave()
	}
	a.active = to
	switch to {
	case tabMain:
		a.main.OnEnter()
	case tabProtos:
		a.protos.OnEnter()
	case tabMappings:
		a.mappings.OnEnter()
	case tabSettings:
		a.settings.OnEnter()
	}
}

func (a *App) layout() {
	tabsH := 2
	statusH := 1
	bodyH := a.height - tabsH - statusH
	if bodyH < 5 {
		bodyH = 5
	}
	a.main.SetSize(a.width, bodyH)
	a.protos.SetSize(a.width, bodyH)
	a.mappings.SetSize(a.width, bodyH)
	a.settings.SetSize(a.width, bodyH)
}

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "initializing..."
	}
	tabs := a.renderTabs()
	var body string
	switch a.active {
	case tabMain:
		body = a.main.View()
	case tabProtos:
		body = a.protos.View()
	case tabMappings:
		body = a.mappings.View()
	case tabSettings:
		body = a.settings.View()
	}
	status := a.renderStatus()
	return lipgloss.JoinVertical(lipgloss.Left, tabs, body, status)
}

func (a *App) renderTabs() string {
	var parts []string
	for i := tabIdx(0); i < tabCount; i++ {
		label := fmt.Sprintf("%d %s", int(i)+1, tabLabels[i])
		if i == a.active {
			parts = append(parts, tabActive.Render(label))
		} else {
			parts = append(parts, tabInactive.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return tabBarStyle.Width(a.width).Render(bar)
}

func (a *App) renderStatus() string {
	left := a.status
	if left == "" {
		left = "ready"
	}
	var bytes uint64
	if a.deps.BytesReceived != nil {
		bytes = a.deps.BytesReceived()
	}
	right := fmt.Sprintf(" port %d  conn v%d  game v%d  %d game IPs  rx %s",
		a.deps.Cfg.ServerPort,
		a.deps.ConnReg.Version(),
		a.deps.GameReg.Version(),
		len(a.deps.Cfg.GameServerIPs),
		humanBytes(bytes),
	)
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	style := statusStyle
	if a.statusLevel == StatusErr {
		style = statusErr
	}
	return style.Width(a.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (a *App) setStatus(level StatusLevel, text string) {
	a.status = text
	a.statusLevel = level
	a.statusAt = time.Now()
}

// --- commands ---

func (a *App) saveConfigCmd() tea.Cmd {
	return func() tea.Msg {
		if err := a.deps.Cfg.Save(); err != nil {
			return StatusMsg{Level: StatusErr, Text: "save config: " + err.Error()}
		}
		return nil
	}
}

func (a *App) reloadWatcherCmd(kind watch.Kind) tea.Cmd {
	return func() tea.Msg {
		var paths []string
		switch kind {
		case watch.KindProto:
			paths = append(paths, a.deps.Cfg.Connection.ProtoPaths...)
			paths = append(paths, a.deps.Cfg.Game.ProtoPaths...)
		case watch.KindMapping:
			paths = a.deps.Cfg.MappingPaths
		}
		if err := a.deps.Watcher.Set(kind, paths); err != nil {
			return StatusMsg{Level: StatusErr, Text: "watcher: " + err.Error()}
		}
		return nil
	}
}

func (a *App) reloadProtosCmd(which RegistryKind) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var (
			reg     *protoreg.Registry
			paths   []string
			envName string
		)
		if which == RegConnection {
			reg = a.deps.ConnReg
			paths = a.deps.Cfg.Connection.ProtoPaths
			envName = a.deps.Cfg.Connection.EnvelopeMessageName
		} else {
			reg = a.deps.GameReg
			paths = a.deps.Cfg.Game.ProtoPaths
			envName = a.deps.Cfg.Game.EnvelopeMessageName
		}
		reg.SetEnvelopeName(envName)
		err := reg.Reload(ctx, paths)
		return RegistryReloadedMsg{
			Which:   which,
			Err:     err,
			Count:   len(paths),
			HasEnv:  reg.HasEnvelope(),
			Version: reg.Version(),
		}
	}
}

func (a *App) reloadMappingsCmd() tea.Cmd {
	return func() tea.Msg {
		err := a.deps.Mappings.Reload(a.deps.Cfg.MappingPaths)
		return MappingsReloadedMsg{Err: err}
	}
}

func (a *App) restartCaptureCmd() tea.Cmd {
	return func() tea.Msg {
		if a.deps.StartCapture == nil {
			return nil
		}
		if err := a.deps.StartCapture(); err != nil {
			return StatusMsg{Level: StatusErr, Text: "capture: " + err.Error()}
		}
		return StatusMsg{Level: StatusInfo, Text: "capture started"}
	}
}

// decodeSelection picks the right registry for the captured message based on
// its Kind (set by the classifier at capture time) and runs Decode.
func (a *App) decodeSelection(it *components.Item) tea.Cmd {
	if it == nil {
		return nil
	}
	seq := it.Seq
	typeUrl := it.Msg.TypeUrl
	value := it.Msg.InnerRaw
	reg := a.registryFor(it.Msg.Kind)
	version := reg.Version()
	return func() tea.Msg {
		if typeUrl == "" {
			errText := "no envelope"
			if it.Msg.Err != nil {
				errText = it.Msg.Err.Error()
			}
			return DecodedMsg{Seq: seq, Version: version, Err: fmt.Errorf("%s", errText)}
		}
		text, err := decode.Decode(reg, a.deps.Mappings, typeUrl, value)
		return DecodedMsg{Seq: seq, Version: version, Text: text, Err: err}
	}
}

func (a *App) registryFor(kind capture.Kind) *protoreg.Registry {
	if kind == capture.KindConnection {
		return a.deps.ConnReg
	}
	return a.deps.GameReg
}
