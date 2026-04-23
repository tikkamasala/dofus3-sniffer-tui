package tui

import "github.com/charmbracelet/lipgloss"

var (
	tabInactive = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("244"))

	tabActive = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("57")).
			Bold(true)

	tabBarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("240"))

	paneDivider = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	statusErr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("235")).
			Padding(0, 1).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	headingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Bold(true)
)
