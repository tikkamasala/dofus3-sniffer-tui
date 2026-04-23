package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func sendStatus(level StatusLevel, text string) tea.Cmd {
	return func() tea.Msg { return StatusMsg{Level: level, Text: text} }
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func humanBytes(n uint64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fGB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.1fMB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1fKB", float64(n)/kb)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
