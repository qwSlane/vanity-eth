package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary = lipgloss.Color("#7C3AED")
	colorAccent  = lipgloss.Color("#06B6D4")
	colorSuccess = lipgloss.Color("#10B981")
	colorDanger  = lipgloss.Color("#EF4444")
	colorMuted   = lipgloss.Color("#6B7280")

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 3).
			Width(58)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	styleDanger = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleAccent = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleStat = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9FAFB"))

	styleKey = lipgloss.NewStyle().
			Foreground(colorDanger)
)
