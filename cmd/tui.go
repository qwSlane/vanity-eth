package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"vanity-eth/internal/tui"
)

func runTUI() error {
	m := tui.New()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
