package main

import "github.com/charmbracelet/lipgloss"

var (
	colorGreen  = lipgloss.Color("#96f97b")
	colorYellow = lipgloss.Color("#ffd93d")
	colorRed    = lipgloss.Color("#ff6b6b")
	colorGray   = lipgloss.Color("#555555")
	colorWhite  = lipgloss.Color("#efefef")
	colorDark   = lipgloss.Color("#1c1c1c")

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGreen).
			PaddingBottom(1)

	styleDim = lipgloss.NewStyle().
			Foreground(colorGray)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorWhite)

	styleWarning = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleError = lipgloss.NewStyle().
			Foreground(colorRed)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray).
			Padding(0, 1)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorGray).
			Width(16)

	styleInputActive = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	styleInputInactive = lipgloss.NewStyle().
				Foreground(colorGray)

	styleHint = lipgloss.NewStyle().
			Foreground(colorGray).
			Italic(true).
			PaddingTop(1)
)

func header(subtitle string) string {
	title := styleHeader.Render("🪰 Zipp")
	if subtitle != "" {
		title += styleHeader.Copy().Bold(false).Foreground(colorGray).Render("  /  " + subtitle)
	}
	ver := styleDim.Render("v" + version)
	gap := lipgloss.NewStyle().Width(40 - lipgloss.Width(title) - lipgloss.Width(ver)).Render("")
	return title + gap + ver + "\n"
}
